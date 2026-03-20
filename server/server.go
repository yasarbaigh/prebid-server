package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/prebid/prebid-server/v3/config"
	"github.com/prebid/prebid-server/v3/logger"
	"github.com/prebid/prebid-server/v3/metrics"
	metricsconfig "github.com/prebid/prebid-server/v3/metrics/config"
)

// Listen blocks forever, serving PBS requests on the given port. This will block forever, until the process is shut down.
func Listen(cfg *config.Configuration, handler http.Handler, adminHandler http.Handler, metrics *metricsconfig.DetailedMetricsEngine) (err error) {
	stopSignals := make(chan os.Signal, 1)
	signal.Notify(stopSignals, syscall.SIGTERM, syscall.SIGINT)

	// Run the servers. Fan any process-stopper signals out to each server for graceful shutdowns.
	stopAdmin := make(chan os.Signal)
	stopPrometheus := make(chan os.Signal)
	
	// Collect all main ports
	mainPorts := []int{cfg.Port}
	if len(cfg.Ports) > 0 {
		mainPorts = cfg.Ports
	}
	
	stopMainChannels := make([]chan os.Signal, len(mainPorts))
	stopChannels := []chan<- os.Signal{}
	
	done := make(chan struct{})

	// Log the port range for the cluster
	if len(mainPorts) > 0 {
		minPort, maxPort := mainPorts[0], mainPorts[0]
		for _, p := range mainPorts {
			if p < minPort {
				minPort = p
			}
			if p > maxPort {
				maxPort = p
			}
		}
		logger.Infof("Accepting connections on ports: [%d - %d] (Total: %d)", minPort, maxPort, len(mainPorts))
	}

	if cfg.UnixSocketEnable && len(cfg.UnixSocketName) > 0 { // start the unix_socket server if config enable-it.
		stopMainChannels[0] = make(chan os.Signal)
		stopChannels = append(stopChannels, stopMainChannels[0])

		mainServer := newSocketServer(cfg, handler)
		go shutdownAfterSignals(mainServer, stopMainChannels[0], done)
		socketListener, err := newUnixListener(mainServer.Addr, metrics)
		if err != nil {
			logger.Errorf("Error listening for Unix-Socket connections on path %s: %v for socket server", mainServer.Addr, err)
			return err
		}
		go runServer(mainServer, "UnixSocket", socketListener)
	} else { // start the TCP servers for all main ports
		for i, port := range mainPorts {
			stopMainChannels[i] = make(chan os.Signal)
			stopChannels = append(stopChannels, stopMainChannels[i])
			
			// Create a copy of the config with the specific port for the server
			portCfg := *cfg
			portCfg.Port = port
			
			mainServer := newMainServer(&portCfg, handler)
			go shutdownAfterSignals(mainServer, stopMainChannels[i], done)
			
			mainListener, err := newTCPListener(mainServer.Addr, metrics)
			if err != nil {
				logger.Errorf("Error listening for TCP connections on %s: %v for main server", mainServer.Addr, err)
				return err
			}
			go runServer(mainServer, fmt.Sprintf("Main:%d", port), mainListener)
		}
	}

	if cfg.Admin.Enabled {
		stopChannels = append(stopChannels, stopAdmin)
		adminServer := newAdminServer(cfg, adminHandler)
		go shutdownAfterSignals(adminServer, stopAdmin, done)

		var adminListener net.Listener
		if adminListener, err = newTCPListener(adminServer.Addr, nil); err != nil {
			logger.Errorf("Error listening for TCP connections on %s: %v for admin server", adminServer.Addr, err)
			return
		}
		go runServer(adminServer, "Admin", adminListener)
	}

	if cfg.Metrics.Prometheus.Port != 0 {
		var (
			prometheusListener net.Listener
			prometheusServer   = newPrometheusServer(cfg, metrics)
		)
		stopChannels = append(stopChannels, stopPrometheus)
		go shutdownAfterSignals(prometheusServer, stopPrometheus, done)
		if prometheusListener, err = newTCPListener(prometheusServer.Addr, nil); err != nil {
			logger.Errorf("Error listening for TCP connections on %s: %v for prometheus server", prometheusServer.Addr, err)
			return
		}

		go runServer(prometheusServer, "Prometheus", prometheusListener)
	}

	wait(stopSignals, done, stopChannels...)

	return
}

func newAdminServer(cfg *config.Configuration, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    cfg.Host + ":" + strconv.Itoa(cfg.AdminPort),
		Handler: handler,
	}
}

func newMainServer(cfg *config.Configuration, handler http.Handler) *http.Server {
	serverHandler := getCompressionEnabledHandler(handler, cfg.Compression.Response)

	return &http.Server{
		Addr:         cfg.Host + ":" + strconv.Itoa(cfg.Port),
		Handler:      serverHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

}

func newSocketServer(cfg *config.Configuration, handler http.Handler) *http.Server {
	serverHandler := getCompressionEnabledHandler(handler, cfg.Compression.Response)

	return &http.Server{
		Addr:         cfg.UnixSocketName,
		Handler:      serverHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
}

func getCompressionEnabledHandler(h http.Handler, compressionInfo config.CompressionInfo) http.Handler {
	if compressionInfo.GZIP {
		h = gziphandler.GzipHandler(h)
	}
	return h
}

func runServer(server *http.Server, name string, listener net.Listener) (err error) {
	if server == nil {
		err = fmt.Errorf(">> Server is a nil_ptr.")
		logger.Errorf("%s server quit with error: %v", name, err)
		return
	} else if listener == nil {
		err = fmt.Errorf(">> Listener is a nil.")
		logger.Errorf("%s server quit with error: %v", name, err)
		return
	}

	logger.Infof("%s server starting on: %s", name, server.Addr)
	if err = server.Serve(listener); err != nil {
		logger.Errorf("%s server quit with error: %v", name, err)
	}
	return
}

func newTCPListener(address string, metrics metrics.MetricsEngine) (net.Listener, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("Error listening for TCP connections on %s: %v", address, err)
	}

	// This cast is in Go's core libs as Server.ListenAndServe(), so it _should_ be safe, but just in case it changes in a future version...
	if casted, ok := ln.(*net.TCPListener); ok {
		ln = &tcpKeepAliveListener{casted}
	} else {
		logger.Warnf("net.Listen(\"tcp\", \"addr\") didn't return a TCPListener as it did in Go 1.9. Things will probably work fine... but this should be investigated.")
	}

	if metrics != nil {
		ln = &monitorableListener{ln, metrics}
	}

	return ln, nil
}

func newUnixListener(address string, metrics metrics.MetricsEngine) (net.Listener, error) {
	ln, err := net.Listen("unix", address)
	if err != nil {
		return nil, fmt.Errorf("Error listening for Unix-Socket connections on path %s: %v", address, err)
	}

	if casted, ok := ln.(*net.UnixListener); ok {
		ln = &unixListener{casted}
	} else {
		logger.Warnf("net.Listen(\"unix\", \"addr\") didn't return an UnixListener.")
	}

	if metrics != nil {
		ln = &monitorableListener{ln, metrics}
	}

	return ln, nil
}

func wait(inbound <-chan os.Signal, done <-chan struct{}, outbound ...chan<- os.Signal) {
	sig := <-inbound

	for i := 0; i < len(outbound); i++ {
		go sendSignal(outbound[i], sig)
	}

	for i := 0; i < len(outbound); i++ {
		<-done
	}
}

func shutdownAfterSignals(server *http.Server, stopper <-chan os.Signal, done chan<- struct{}) {
	sig := <-stopper

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var s struct{}
	logger.Infof("Stopping %s because of signal: %s", server.Addr, sig.String())
	if err := server.Shutdown(ctx); err != nil {
		logger.Errorf("Failed to shutdown %s: %v", server.Addr, err)
	}
	done <- s
}

func sendSignal(to chan<- os.Signal, sig os.Signal) {
	to <- sig
}
