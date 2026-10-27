package proxy

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/shaomian/PrismCat/internal/trace"
)

// wsFrames observes a copy of the wire stream. It never serializes or writes a
// frame; parse failures disable observation while the original tunnel continues.
type wsFrames struct {
	client, deflate, noContext  bool
	buffer, message, dictionary []byte
	opcode                      byte
	compressed                  bool
}

func (f *wsFrames) configure(extension string) error {
	if extension == "" {
		return nil
	}
	parts := strings.Split(extension, ";")
	if strings.TrimSpace(parts[0]) != "permessage-deflate" || strings.Contains(extension, ",") {
		return fmt.Errorf("unsupported_websocket_extension")
	}
	f.deflate = true
	for _, part := range parts[1:] {
		parameter := strings.TrimSpace(part)
		switch {
		case parameter == "client_no_context_takeover":
			if f.client {
				f.noContext = true
			}
		case parameter == "server_no_context_takeover":
			if !f.client {
				f.noContext = true
			}
		case strings.HasPrefix(parameter, "client_max_window_bits="), strings.HasPrefix(parameter, "server_max_window_bits="):
			// The decoder's 32 KiB window can read smaller negotiated windows.
		default:
			return fmt.Errorf("unsupported_websocket_extension")
		}
	}
	return nil
}

func (f *wsFrames) feed(chunk []byte, emit func([]byte) error) error {
	f.buffer = append(f.buffer, chunk...)
	for len(f.buffer) >= 2 {
		b := f.buffer
		fin, compressed, opcode := b[0]&128 != 0, b[0]&64 != 0, b[0]&15
		masked := b[1]&128 != 0
		if b[0]&48 != 0 || masked != f.client || (compressed && (!f.deflate || opcode == 0 || opcode >= 8)) {
			return fmt.Errorf("invalid_websocket_frame")
		}
		n, offset := uint64(b[1]&127), 2
		if n == 126 {
			if len(b) < 4 {
				return nil
			}
			n, offset = uint64(binary.BigEndian.Uint16(b[2:4])), 4
		} else if n == 127 {
			if len(b) < 10 {
				return nil
			}
			n, offset = binary.BigEndian.Uint64(b[2:10]), 10
		}
		if n > trace.MaxParseBytes || uint64(len(f.message))+n > trace.MaxParseBytes {
			return fmt.Errorf("websocket_message_limit")
		}
		maskStart := offset
		if masked {
			offset += 4
		}
		if uint64(len(b)) < uint64(offset)+n {
			return nil
		}
		payload := b[offset : offset+int(n)]
		if masked {
			for i := range payload {
				payload[i] ^= b[maskStart+i%4]
			}
		}
		if opcode >= 8 {
			if !fin || n > 125 || (opcode != 8 && opcode != 9 && opcode != 10) {
				return fmt.Errorf("invalid_websocket_control")
			}
		} else {
			if opcode != 0 {
				if f.opcode != 0 || (opcode != 1 && opcode != 2) {
					return fmt.Errorf("invalid_websocket_fragment")
				}
				f.opcode, f.compressed = opcode, compressed
			} else if f.opcode == 0 {
				return fmt.Errorf("invalid_websocket_fragment")
			}
			f.message = append(f.message, payload...)
			if fin {
				message := f.message
				if f.compressed {
					// RFC 7692 removes the sync-flush tail. Add it and a final
					// empty block so the bounded flate reader reaches EOF.
					compressedData := append(append([]byte{}, message...), 0, 0, 255, 255, 1, 0, 0, 255, 255)
					reader := flate.NewReaderDict(bytes.NewReader(compressedData), f.dictionary)
					decoded, err := io.ReadAll(io.LimitReader(reader, trace.MaxParseBytes+1))
					_ = reader.Close()
					if err != nil || len(decoded) > trace.MaxParseBytes {
						return fmt.Errorf("websocket_decompression_failed")
					}
					message = decoded
					if !f.noContext {
						f.dictionary = append(f.dictionary, message...)
						if len(f.dictionary) > 32768 {
							f.dictionary = append([]byte{}, f.dictionary[len(f.dictionary)-32768:]...)
						}
					}
				}
				if f.opcode != 1 {
					return fmt.Errorf("unsupported_websocket_binary")
				}
				if err := emit(message); err != nil {
					return err
				}
				f.message, f.opcode, f.compressed = nil, 0, false
			}
		}
		f.buffer = f.buffer[offset+int(n):]
	}
	return nil
}
