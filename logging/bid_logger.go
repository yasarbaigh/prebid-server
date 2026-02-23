package logging

import (
	"encoding/hex"
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
	logChan  chan *generated.AuctionEvent
	filePath string
	writer   *lumberjack.Logger
	mu       sync.Mutex
	once     sync.Once
	hostname string
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

	hostname, _ := os.Hostname()

	instance = &BidLogger{
		logChan:  make(chan *generated.AuctionEvent, bufferSize),
		filePath: path,
		writer:   lumberjackLogger,
		hostname: hostname,
	}

	go instance.start()
	return nil
}

func (l *BidLogger) start() {
	for event := range l.logChan {
		l.writeEvent(event)
	}
}

func (l *BidLogger) writeEvent(event *generated.AuctionEvent) {
	data, err := proto.Marshal(event)
	if err != nil {
		logger.Errorf("Failed to marshal auction event: %v", err)
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Hex-encode and add newline
	hexData := hex.EncodeToString(data)
	if _, err := l.writer.Write([]byte(hexData + "\n")); err != nil {
		logger.Errorf("Failed to write hex data to log: %v", err)
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

func (l *BidLogger) Close() {
	close(l.logChan)
	if l.writer != nil {
		l.writer.Close()
	}
}
