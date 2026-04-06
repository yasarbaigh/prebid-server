package logging

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/magiconair/properties"
	"github.com/prebid/prebid-server/v3/logger"
	"github.com/prebid/prebid-server/v3/proto/generated"
	"google.golang.org/protobuf/proto"
	"gopkg.in/natefinch/lumberjack.v2"
)

func getInstanceID() string {
	if val := os.Getenv("INSTANCE_ID"); val != "" {
		return val
	}
	if val := os.Getenv("NODE_APP_INSTANCE"); val != "" {
		// Cluster mode support
		return val
	}
	return "1"
}

func formatLogPath(path, hostname, instanceID string) string {
	if path == "" {
		return path
	}
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	// If the user hasn't added a placeholder, we append the instance ID
	return fmt.Sprintf("%s_%s%s", base, instanceID, ext)
}

type BidLogger struct {
	logChan           chan *generated.AuctionEvent
	filePath          string
	writer            *lumberjack.Logger
	bufWriter         *bufio.Writer
	mu                sync.Mutex
	once              sync.Once
	hostname          string
	instanceID        string
	verboseLogEnabled bool
	verboseLogPath    string
	verboseMaxMB      int
	verboseBackups    int
	verboseChan       chan *verboseEvent
	verboseLoggers    map[string]*lumberjack.Logger
	verboseSampleRate float64
	vMu               sync.Mutex
	wg                sync.WaitGroup
	exchangeOverhead  int
}

var (
	eventPool = sync.Pool{
		New: func() interface{} {
			return &generated.AuctionEvent{}
		},
	}
	// Pool for base64 encoded lines to avoid allocations
	linePool = sync.Pool{
		New: func() interface{} {
			// Pre-allocate a reasonable buffer for RTB logs
			return make([]byte, 8192)
		},
	}
)

func GetEventFromPool() *generated.AuctionEvent {
	e := eventPool.Get().(*generated.AuctionEvent)
	e.Reset()
	return e
}

func ReleaseEventToPool(e *generated.AuctionEvent) {
	if e != nil {
		eventPool.Put(e)
	}
}

type verboseEvent struct {
	id    string
	data  []byte
	isSSp bool
	label string
}

var instance *BidLogger

func GetBidLogger() *BidLogger {
	return instance
}

func InitBidLogger(propsPath string) error {
	p, err := properties.LoadFile(propsPath, properties.UTF8)
	if err != nil {
		return fmt.Errorf("failed to load %s: %v", propsPath, err)
	}

	hostname, _ := os.Hostname()
	instanceID := getInstanceID()

	path := p.GetString("logging.auction_log.path", "/opt/adserving/logs/auction_events.pb.log")
	path = formatLogPath(path, hostname, instanceID)

	bufferSize := p.GetInt("logging.auction_log.channel_buffer", 10000)
	maxSize := p.GetInt("logging.auction_log.max_file_size_mb", 100)
	maxBackups := p.GetInt("logging.auction_log.max_backups", 5)

	// Service Log initialization
	serviceLogPath := p.GetString("service_log", "/opt/adserving/logs/pbs_service.log")
	serviceLogPath = formatLogPath(serviceLogPath, hostname, instanceID)

	serviceLogLevel := p.GetString("service_log_level", "INFO")
	serviceLogMaxSize := p.GetInt("service_log_max_size", 100)
	serviceLogMaxBackups := p.GetInt("service_log_max_backups", 5)
	serviceLogMaxAge := p.GetInt("service_log_max_age", 30)
	serviceLogCompress := p.GetBool("service_log_compress", true)

	// Ensure directory exists for service log
	serviceDir := filepath.Dir(serviceLogPath)
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		logger.Warnf("Failed to create service log directory %s: %v", serviceDir, err)
	}

	// Initialize global service logger
	serviceLogger := logger.NewServiceLogger(serviceLogPath, serviceLogMaxSize, serviceLogMaxBackups, serviceLogMaxAge, serviceLogCompress, serviceLogLevel)
	logger.SetLogger(serviceLogger)

	// Ensure auction log directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Warnf("Failed to create log directory %s: %v", dir, err)
	}

	lumberjackLogger := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    maxSize, // megabytes
		MaxBackups: maxBackups,
		LocalTime:  true,
		Compress:   true,
	}

	verboseLogEnabled := p.GetBool("verbose_log", false)
	verboseLogPath := p.GetString("verbose_log.path", "/opt/adserving/verbose")
	vMaxMB := p.GetInt("verbose_log.max_file_size_mb", 10)
	vMaxBackups := p.GetInt("verbose_log.max_backups", 5)
	vSampleRate := p.GetFloat64("verbose_log.sample_rate", 0.0)
	overhead := p.GetInt("exchange.overhead_ms", 120)

	// Ensure verbose directory exists if enabled
	if verboseLogEnabled {
		if err := os.MkdirAll(verboseLogPath, 0755); err != nil {
			logger.Warnf("Failed to create verbose log directory %s: %v", verboseLogPath, err)
		}
	}

	instance = &BidLogger{
		logChan:           make(chan *generated.AuctionEvent, bufferSize),
		filePath:          path,
		writer:            lumberjackLogger,
		hostname:          hostname,
		instanceID:        instanceID,
		verboseLogEnabled: verboseLogEnabled,
		verboseLogPath:    verboseLogPath,
		verboseMaxMB:      vMaxMB,
		verboseBackups:    vMaxBackups,
		verboseLoggers:    make(map[string]*lumberjack.Logger),
		verboseSampleRate: vSampleRate,
		bufWriter:         bufio.NewWriterSize(lumberjackLogger, 256*1024),
		exchangeOverhead:  overhead,
	}

	if verboseLogEnabled {
		instance.verboseChan = make(chan *verboseEvent, bufferSize)
	}

	instance.wg.Add(1)
	go instance.start()
	return nil
}

func (l *BidLogger) start() {
	if l.verboseLogEnabled {
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			for event := range l.verboseChan {
				var filename string
				if event.isSSp {
					filename = fmt.Sprintf("ssp_%s.log", event.id)
				} else {
					filename = fmt.Sprintf("dsp_%s.log", event.id)
				}
				l.appendToVerboseFile(filename, event.data, event.label)
			}
		}()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	defer l.wg.Done()
	for {
		select {
		case event, ok := <-l.logChan:
			if !ok {
				// Channel closed, final flush before exiting
				l.bufWriter.Flush()
				return
			}
			l.writeEvent(event)
			// Release the event back to pool after write is finished
			ReleaseEventToPool(event)
		case <-ticker.C:
			// Periodic flush for long-tail RTB instances
			l.bufWriter.Flush()
		}
	}
}

func (l *BidLogger) writeVerbose(event *generated.AuctionEvent) {
	if !l.verboseLogEnabled {
		return
	}

	if event.SspPartnerId != 0 && len(event.RawBidRequest) > 0 {
		// Note: AuctionEvent currently only has numeric IDs.
		// For consistency, we'll continue using numeric IDs here or update AuctionEvent proto.
		// However, LogSSP/LogDSP are the primary entry points for verbose logging.
		filename := fmt.Sprintf("ssp_%d.log", event.SspPartnerId)
		l.appendToVerboseFile(filename, event.RawBidRequest, "REQ")
	}

	if event.DspPartnerId != 0 && len(event.SspDspResponse) > 0 {
		filename := fmt.Sprintf("dsp_%d.log", event.DspPartnerId)
		l.appendToVerboseFile(filename, event.SspDspResponse, "RESP")
	}
}

func (l *BidLogger) appendToVerboseFile(filename string, data []byte, label string) {
	l.vMu.Lock()
	writer, ok := l.verboseLoggers[filename]
	if !ok {
		suffixedFilename := formatLogPath(filename, l.hostname, l.instanceID)
		path := fmt.Sprintf("%s/%s", l.verboseLogPath, suffixedFilename)
		writer = &lumberjack.Logger{
			Filename:   path,
			MaxSize:    l.verboseMaxMB,
			MaxBackups: l.verboseBackups,
			LocalTime:  true,
			Compress:   true,
		}
		l.verboseLoggers[filename] = writer
	}
	l.vMu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, label, string(data))
	writer.Write([]byte(line))
}

func (l *BidLogger) writeEvent(event *generated.AuctionEvent) {
	data, err := proto.Marshal(event)
	if err != nil {
		logger.Errorf("Failed to marshal auction event: %v", err)
		return
	}

	encodedLen := base64.StdEncoding.EncodedLen(len(data))

	// Get a buffer from the line pool
	buf := linePool.Get().([]byte)
	if len(buf) < encodedLen+1 {
		buf = make([]byte, encodedLen+1)
	}

	base64.StdEncoding.Encode(buf, data)
	buf[encodedLen] = '\n'

	// No Lock needed here as only the 'start()' goroutine calls this
	if _, err := l.bufWriter.Write(buf[:encodedLen+1]); err != nil {
		logger.Errorf("Failed to write base64 data to log: %v", err)
	}

	// Return the buffer to the line pool
	linePool.Put(buf)
}

func (l *BidLogger) Log(event *generated.AuctionEvent) {
	event.Hostname = l.hostname
	event.Timestamp = time.Now().UnixMilli()

	select {
	case l.logChan <- event:
	default:
		logger.Warnf("BidLogger channel full, dropping event for auction %s", event.SspPartnerAuctionId)
	}
}

func (l *BidLogger) LogSSP(sspIdentifier string, body []byte, label string) {
	if !l.verboseLogEnabled || l.verboseChan == nil {
		return
	}

	select {
	case l.verboseChan <- &verboseEvent{id: sspIdentifier, data: body, isSSp: true, label: label}:
	default:
		// Drop silently for verbose checking
	}
}

func (l *BidLogger) LogDSP(dspIdentifier string, body []byte, label string) {
	if !l.verboseLogEnabled || l.verboseChan == nil {
		return
	}

	select {
	case l.verboseChan <- &verboseEvent{id: dspIdentifier, data: body, isSSp: false, label: label}:
	default:
		// Drop silently for verbose checking
	}
}

func (l *BidLogger) ShouldSampleVerbose() bool {
	if !l.verboseLogEnabled || l.verboseChan == nil || l.verboseSampleRate <= 0 {
		return false
	}
	return rand.Float64() < l.verboseSampleRate
}

func (l *BidLogger) LogSSPSampled(sspIdentifier string, body []byte, label string) {
	if !l.verboseLogEnabled || l.verboseChan == nil || l.verboseSampleRate <= 0 {
		return
	}

	if rand.Float64() < l.verboseSampleRate {
		l.LogSSP(sspIdentifier, body, label)
	}
}

func (l *BidLogger) LogDSPSampled(dspIdentifier string, body []byte, label string) {
	if !l.verboseLogEnabled || l.verboseChan == nil || l.verboseSampleRate <= 0 {
		return
	}

	if rand.Float64() < l.verboseSampleRate {
		l.LogDSP(dspIdentifier, body, label)
	}
}

func (l *BidLogger) GetExchangeOverhead() int {
	if l == nil {
		return 120
	}
	return l.exchangeOverhead
}

func (l *BidLogger) IsVerboseEnabled() bool {
	if l == nil {
		return false
	}
	return l.verboseLogEnabled
}

func (l *BidLogger) Close() {
	if l == nil {
		return
	}

	// First close channels to signal workers to stop AFTER they drain the buffer
	close(l.logChan)
	if l.verboseChan != nil {
		close(l.verboseChan)
	}

	// Wait for workers to finish draining
	l.wg.Wait()

	if l.bufWriter != nil {
		l.bufWriter.Flush()
	}

	l.vMu.Lock()
	defer l.vMu.Unlock()
	for _, v := range l.verboseLoggers {
		v.Close()
	}

	if l.writer != nil {
		l.writer.Close()
	}
}
