package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Типы пакетов
const (
	PacketTypeConnect    = 0x01
	PacketTypeData       = 0x02
	PacketTypeDisconnect = 0x03
	PacketTypeHeartbeat  = 0x04
)

// Packet представляет пакет туннельного протокола
type Packet struct {
	Type      uint8
	SessionID uint32
	Data      []byte
}

// Encode кодирует пакет в байты
func (p *Packet) Encode() ([]byte, error) {
	buf := make([]byte, 9+len(p.Data))
	
	// Magic bytes (2 bytes) - для идентификации протокола
	buf[0] = 0x54 // 'T'
	buf[1] = 0x50 // 'P'
	
	// Type (1 byte)
	buf[2] = p.Type
	
	// SessionID (4 bytes)
	binary.BigEndian.PutUint32(buf[3:7], p.SessionID)
	
	// Data length (2 bytes)
	binary.BigEndian.PutUint16(buf[7:9], uint16(len(p.Data)))
	
	// Data
	copy(buf[9:], p.Data)
	
	return buf, nil
}

// Decode декодирует пакет из байтов
func Decode(r io.Reader) (*Packet, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	
	// Проверка magic bytes
	if header[0] != 0x54 || header[1] != 0x50 {
		return nil, fmt.Errorf("invalid magic bytes")
	}
	
	p := &Packet{
		Type:      header[2],
		SessionID: binary.BigEndian.Uint32(header[3:7]),
	}
	
	dataLen := binary.BigEndian.Uint16(header[7:9])
	if dataLen > 0 {
		p.Data = make([]byte, dataLen)
		if _, err := io.ReadFull(r, p.Data); err != nil {
			return nil, err
		}
	}
	
	return p, nil
}

// NewConnectPacket создает пакет подключения
func NewConnectPacket(sessionID uint32, targetAddr string) *Packet {
	return &Packet{
		Type:      PacketTypeConnect,
		SessionID: sessionID,
		Data:      []byte(targetAddr),
	}
}

// NewDataPacket создает пакет с данными
func NewDataPacket(sessionID uint32, data []byte) *Packet {
	return &Packet{
		Type:      PacketTypeData,
		SessionID: sessionID,
		Data:      data,
	}
}

// NewDisconnectPacket создает пакет отключения
func NewDisconnectPacket(sessionID uint32) *Packet {
	return &Packet{
		Type:      PacketTypeDisconnect,
		SessionID: sessionID,
	}
}

// NewHeartbeatPacket создает heartbeat пакет
func NewHeartbeatPacket() *Packet {
	return &Packet{
		Type:      PacketTypeHeartbeat,
		SessionID: 0,
	}
}
