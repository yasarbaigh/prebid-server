package logging

import (
	"encoding/base64"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/magiconair/properties"
	"github.com/prebid/prebid-server/v3/logger"
	"github.com/prebid/prebid-server/v3/proto/generated"
	"google.golang.org/protobuf/proto"
	"gopkg.in/natefinch/lumberjack.v2"
)

type BidLogger struct {
	logChan           chan *generated.AuctionEvent
	filePath          string
	writer            *lumberjack.Logger
	mu                sync.Mutex
	once              sync.Once
	hostname          string
	verboseLogEnabled bool
	verboseLogPath    string
	verboseMaxMB      int
	verboseBackups    int
	verboseChan       chan *verboseEvent
	verboseLoggers    map[string]*lumberjack.Logger
	vMu               sync.Mutex
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

	path := p.GetString("logging.bid_combo.path", "/opt/adserving/logs/auction_events.pb.log")
	bufferSize := p.GetInt("logging.bid_combo.channel_buffer", 10000)
	maxSize := p.GetInt("logging.bid_combo.max_file_size_mb", 100)
	maxBackups := p.GetInt("logging.bid_combo.max_backups", 5)

	// Ensure directory exists
	dir := "/opt/adserving/logs"
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
	hostname, _ := os.Hostname()

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
		verboseLogEnabled: verboseLogEnabled,
		verboseLogPath:    verboseLogPath,
		verboseMaxMB:      vMaxMB,
		verboseBackups:    vMaxBackups,
		verboseLoggers:    make(map[string]*lumberjack.Logger),
	}

	if verboseLogEnabled {
		instance.verboseChan = make(chan *verboseEvent, bufferSize)
	}

	go instance.start()
	return nil
}

func (l *BidLogger) start() {
	if l.verboseLogEnabled {
		go func() {
			for event := range l.verboseChan {
				var filename string
				if event.isSSp {
					filename = fmt.Sprintf("%s_ssp.log", event.id)
				} else {
					filename = fmt.Sprintf("%s_dsp.log", event.id)
				}
				l.appendToVerboseFile(filename, event.data, event.label)
			}
		}()
	}

	for event := range l.logChan {
		l.writeEvent(event)
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
		path := fmt.Sprintf("%s/%s", l.verboseLogPath, filename)
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
	out := make([]byte, encodedLen+1)
	base64.StdEncoding.Encode(out, data)
	out[encodedLen] = '\n'

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, err := l.writer.Write(out); err != nil {
		logger.Errorf("Failed to write base64 data to log: %v", err)
	}
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

func (l *BidLogger) Close() {
	close(l.logChan)
	if l.verboseChan != nil {
		close(l.verboseChan)
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
