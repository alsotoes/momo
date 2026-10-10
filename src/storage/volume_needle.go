package storage

import (
	"encoding/binary"
	"hash/crc32"
	"os"
)

// Needle constants
const (
	NeedleMagic       = 0x4D4F4D4F // "MOMO"
	NeedleMagicBytes  = 4
	NeedleLengthBytes = 4
	NeedleHashBytes   = 32
	NeedleFlagsBytes  = 2
	NeedleCRCBytes    = 4
	NeedleHeaderSize  = NeedleMagicBytes + NeedleLengthBytes + NeedleHashBytes + NeedleFlagsBytes
	NeedleFooterSize  = NeedleCRCBytes
	NeedleOverhead    = NeedleHeaderSize + NeedleFooterSize // 4+4+32+2+4 = 46 bytes
	NeedleAlignment   = 8
)

// NeedleFlags represents the flags byte in a needle
type NeedleFlags uint16

const (
	NeedleFlagNone       NeedleFlags = 0
	NeedleFlagTombstone  NeedleFlags = 1 << 0 // needle is deleted (tombstone)
	NeedleFlagCompressed NeedleFlags = 1 << 1 // payload is compressed (future)
	NeedleFlagEncrypted  NeedleFlags = 1 << 2 // payload is encrypted (future)
)

// NeedleHeader represents the header of a needle in a volume file
type NeedleHeader struct {
	Magic      uint32      // Must be NeedleMagic (0x4D4F4D4F)
	PayloadLen uint32      // Length of payload in bytes (excluding header/footer)
	Hash       [32]byte    // SHA-256 hash of payload
	Flags      NeedleFlags // Flags (tombstone, compressed, etc.)
}

// NeedleTrailer represents the footer of a needle
type NeedleTrailer struct {
	CRC32 uint32 // CRC32C checksum of (Magic || Length || Hash || Flags || Payload)
}

// Needle represents a complete needle in a volume file
type Needle struct {
	Header  NeedleHeader
	Payload []byte
	Trailer NeedleTrailer
}

// Volume file constants
const (
	DefaultVolumeSize = 1 << 30  // 1 GiB
	MaxVolumeSize     = 32 << 30 // 32 GiB max
	VolumeFilePrefix  = "vol-"
	VolumeFileExt     = ".dat"
)

// alignTo8 returns the next multiple of 8 >= n
func alignTo8(n int) int {
	return (n + NeedleAlignment - 1) &^ (NeedleAlignment - 1)
}

// writeNeedle writes a needle to the volume file at the current position
// Returns the number of bytes written (including padding)
func writeNeedle(file *os.File, hash [32]byte, payload []byte, flags NeedleFlags) (int64, error) {
	payloadLen := len(payload)
	alignedPayloadLen := alignTo8(payloadLen)
	padding := alignedPayloadLen - payloadLen

	header := NeedleHeader{
		Magic:      NeedleMagic,
		PayloadLen: uint32(payloadLen),
		Hash:       hash,
		Flags:      flags,
	}

	// Calculate CRC32C over header + payload
	crc := crc32.MakeTable(crc32.Castagnoli)
	data := make([]byte, NeedleHeaderSize+payloadLen)
	binary.BigEndian.PutUint32(data[0:4], header.Magic)
	binary.BigEndian.PutUint32(data[4:8], header.PayloadLen)
	copy(data[8:40], header.Hash[:])
	binary.BigEndian.PutUint16(data[40:42], uint16(header.Flags))
	copy(data[42:42+payloadLen], payload)
	crcVal := crc32.Checksum(data, crc)

	// Write header
	if err := binary.Write(file, binary.BigEndian, header.Magic); err != nil {
		return 0, err
	}
	if err := binary.Write(file, binary.BigEndian, header.PayloadLen); err != nil {
		return 0, err
	}
	if _, err := file.Write(header.Hash[:]); err != nil {
		return 0, err
	}
	if err := binary.Write(file, binary.BigEndian, uint16(header.Flags)); err != nil {
		return 0, err
	}

	// Write payload
	if _, err := file.Write(payload); err != nil {
		return 0, err
	}

	// Write padding
	if padding > 0 {
		if _, err := file.Write(make([]byte, padding)); err != nil {
			return 0, err
		}
	}

	// Write CRC
	if err := binary.Write(file, binary.BigEndian, crcVal); err != nil {
		return 0, err
	}

	totalWritten := int64(NeedleHeaderSize + alignedPayloadLen + NeedleCRCBytes)
	return totalWritten, nil
}

// readNeedle reads a needle from the volume file at the given offset
func readNeedle(file *os.File, offset int64) (*Needle, error) {
	buf := make([]byte, NeedleHeaderSize)
	if _, err := file.ReadAt(buf, offset); err != nil {
		return nil, err
	}
	header := NeedleHeader{
		Magic:      binary.BigEndian.Uint32(buf[0:4]),
		PayloadLen: binary.BigEndian.Uint32(buf[4:8]),
		Flags:      NeedleFlags(binary.BigEndian.Uint16(buf[40:42])),
	}
	copy(header.Hash[:], buf[8:40])

	if header.Magic != NeedleMagic {
		return nil, ErrInvalidNeedleMagic
	}

	payloadLen := int(header.PayloadLen)
	alignedPayloadLen := alignTo8(payloadLen)

	// Read payload
	payload := make([]byte, int(header.PayloadLen))
	if _, err := file.ReadAt(payload, offset+int64(NeedleHeaderSize)); err != nil {
		return nil, err
	}

	// Read CRC
	crcBuf := make([]byte, NeedleCRCBytes)
	crcOffset := offset + int64(NeedleHeaderSize) + int64(alignedPayloadLen)
	if _, err := file.ReadAt(crcBuf, crcOffset); err != nil {
		return nil, err
	}
	crcVal := binary.BigEndian.Uint32(crcBuf)

	// Verify CRC
	crc := crc32.MakeTable(crc32.Castagnoli)
	data := make([]byte, NeedleHeaderSize+len(payload))
	binary.BigEndian.PutUint32(data[0:4], NeedleMagic)
	binary.BigEndian.PutUint32(data[4:8], header.PayloadLen)
	copy(data[8:40], header.Hash[:])
	binary.BigEndian.PutUint16(data[40:42], uint16(header.Flags))
	copy(data[42:42+len(payload)], payload)
	expectedCRC := crc32.Checksum(data, crc)
	if crcVal != expectedCRC {
		return nil, ErrCRC32Mismatch
	}

	return &Needle{
		Header:  header,
		Payload: payload,
		Trailer: NeedleTrailer{CRC32: crcVal},
	}, nil
}

// Custom errors
var (
	ErrInvalidNeedleMagic = &storageError{"invalid needle magic"}
	ErrCRC32Mismatch      = &storageError{"CRC32 mismatch"}
)

type storageError struct {
	msg string
}

func (e *storageError) Error() string {
	return e.msg
}
