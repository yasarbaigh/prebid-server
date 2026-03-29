package cryptoutil

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/gofrs/uuid"
)

// Mapping Enums to match OpenRTB 2.5/2.6 Standards
type DeviceType uint8
const (
	DT_Unknown         DeviceType = 0
	DT_Mobile          DeviceType = 1 // Mobile/Tablet
	DT_PC              DeviceType = 2 // Personal Computer
	DT_CTV             DeviceType = 3 // Connected TV
	DT_Phone           DeviceType = 4 // Phone
	DT_Tablet          DeviceType = 5 // Tablet
	DT_ConnectedDevice DeviceType = 6 // Connected Device
	DT_SetTopBox       DeviceType = 7 // Set Top Box
)

type OS uint8
const (
	OS_Unknown OS = 0
	OS_Android OS = 1
	OS_iOS     OS = 2
	OS_Windows OS = 3
	OS_MacOS   OS = 4
	OS_Linux   OS = 5
)

type AdType uint8
const (
	AT_Unknown AdType = 0
	AT_Banner  AdType = 1
	AT_Video   AdType = 2
	AT_Native  AdType = 3
	AT_Audio   AdType = 4
)

// PositionalData contains all metadata for a winning bid or tracker.
type PositionalData struct {
	Timestamp      uint32
	TenantID       uint32
	SSPID          uint32
	SSPInventoryID uint32
	DSPID          uint32
	DSPInventoryID uint32
	Price          float64
	DeviceType     DeviceType
	OS             OS
	OSV            string
	Country        string
	AdType         AdType
	AdSize         string
	Domain         string
	BundleID       string
	Carrier        string
	AuctionID      string // UUID
	BidID          string // UUID
	ImpID          string // UUID
	Seat           string
	AdID           string
}

// Pack converts the struct into a tight Big-Endian binary buffer.
func (p *PositionalData) Pack() ([]byte, error) {
	buf := new(bytes.Buffer)

	// 1. Fixed Width Block (35 Bytes)
	binary.Write(buf, binary.BigEndian, p.Timestamp)
	binary.Write(buf, binary.BigEndian, p.TenantID)
	binary.Write(buf, binary.BigEndian, p.SSPID)
	binary.Write(buf, binary.BigEndian, p.SSPInventoryID)
	binary.Write(buf, binary.BigEndian, p.DSPID)
	binary.Write(buf, binary.BigEndian, p.DSPInventoryID)
	binary.Write(buf, binary.BigEndian, p.Price) // Double (8 bytes)
	binary.Write(buf, binary.BigEndian, uint8(p.DeviceType))
	binary.Write(buf, binary.BigEndian, uint8(p.OS))
	binary.Write(buf, binary.BigEndian, uint8(p.AdType))

	// 2. String Block (All Length Prefixed)
	writeLPString(buf, p.AuctionID)
	writeLPString(buf, p.BidID)
	writeLPString(buf, p.ImpID)
	writeLPString(buf, p.OSV)
	writeLPString(buf, p.Country)
	writeLPString(buf, p.AdSize)
	writeLPString(buf, p.Domain)
	writeLPString(buf, p.BundleID)
	writeLPString(buf, p.Carrier)
	writeLPString(buf, p.Seat)
	writeLPString(buf, p.AdID)

	return buf.Bytes(), nil
}

// Unpack recreates the struct from the binary buffer.
func Unpack(data []byte) (*PositionalData, error) {
	reader := bytes.NewReader(data)
	p := &PositionalData{}

	// 1. Fixed Width Block
	var ts, tid, sid, siid, did, diid uint32
	var dt, os, at uint8
	
	if err := binary.Read(reader, binary.BigEndian, &ts); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &tid); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &sid); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &siid); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &did); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &diid); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &p.Price); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &dt); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &os); err != nil { return nil, err }
	if err := binary.Read(reader, binary.BigEndian, &at); err != nil { return nil, err }

	p.Timestamp = ts
	p.TenantID = tid
	p.SSPID = sid
	p.SSPInventoryID = siid
	p.DSPID = did
	p.DSPInventoryID = diid
	// p.Price already set
	p.DeviceType = DeviceType(dt)
	p.OS = OS(os)
	p.AdType = AdType(at)

	// 2. Strings
	p.AuctionID = readLPString(reader)
	p.BidID = readLPString(reader)
	p.ImpID = readLPString(reader)
	p.OSV = readLPString(reader)
	p.Country = readLPString(reader)
	p.AdSize = readLPString(reader)
	p.Domain = readLPString(reader)
	p.BundleID = readLPString(reader)
	p.Carrier = readLPString(reader)
	p.Seat = readLPString(reader)
	p.AdID = readLPString(reader)

	return p, nil
}

func packUUID(buf *bytes.Buffer, id string) {
	u, err := uuid.FromString(id)
	if err != nil {
		buf.Write(make([]byte, 16)) // Fallback to empty
		return
	}
	buf.Write(u.Bytes())
}

func unpackUUID(r *bytes.Reader) string {
	b := make([]byte, 16)
	r.Read(b)
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func writeLPString(buf *bytes.Buffer, s string) {
	binary.Write(buf, binary.BigEndian, uint16(len(s)))
	buf.WriteString(s)
}

func readLPString(r *bytes.Reader) string {
	var length uint16
	binary.Read(r, binary.BigEndian, &length)
	if length == 0 {
		return ""
	}
	b := make([]byte, length)
	r.Read(b)
	return string(b)
}

// Helpers for strict mapping
func GetOsEnum(os string) OS {
	val := strings.ToLower(os)
	switch {
	case strings.Contains(val, "android"): return OS_Android
	case strings.Contains(val, "ios") || strings.Contains(val, "iphone"): return OS_iOS
	case strings.Contains(val, "windows"): return OS_Windows
	case strings.Contains(val, "mac"): return OS_MacOS
	case strings.Contains(val, "linux"): return OS_Linux
	default: return OS_Unknown
	}
}

func GetDeviceTypeEnum(dt string) DeviceType {
	val := strings.ToLower(dt)
	switch {
	case strings.Contains(val, "phone"): return DT_Phone
	case strings.Contains(val, "tablet") && !strings.Contains(val, "mobile"): return DT_Tablet
	case strings.Contains(val, "mobile"): return DT_Mobile
	case strings.Contains(val, "personal computer") || strings.Contains(val, "pc"): return DT_PC
	case strings.Contains(val, "tv") || strings.Contains(val, "ctv"): return DT_CTV
	case strings.Contains(val, "connected device"): return DT_ConnectedDevice
	case strings.Contains(val, "set top box") || strings.Contains(val, "stb"): return DT_SetTopBox
	default: return DT_Unknown
	}
}

func GetAdTypeEnum(at string) AdType {
	switch strings.ToLower(at) {
	case "banner": return AT_Banner
	case "video": return AT_Video
	case "native": return AT_Native
	case "audio": return AT_Audio
	default: return AT_Unknown
	}
}
