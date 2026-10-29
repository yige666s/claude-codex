package upstreamproxy

// EncodeChunk encodes an UpstreamProxyChunk protobuf message by hand.
//
// For `message UpstreamProxyChunk { bytes data = 1; }` the wire format is:
//   tag = (field_number << 3) | wire_type = (1 << 3) | 2 = 0x0a
//   followed by varint length, followed by the bytes.
func EncodeChunk(data []byte) []byte {
	length := len(data)

	// Encode varint length
	varint := encodeVarint(length)

	// Build output: tag + varint + data
	out := make([]byte, 1+len(varint)+length)
	out[0] = 0x0a // tag for field 1, wire type 2 (length-delimited)
	copy(out[1:], varint)
	copy(out[1+len(varint):], data)

	return out
}

// DecodeChunk decodes an UpstreamProxyChunk protobuf message.
// Returns the data field, or nil if malformed.
// Tolerates zero-length chunks (keepalive semantics).
func DecodeChunk(buf []byte) []byte {
	if len(buf) == 0 {
		return []byte{}
	}

	// Check tag
	if buf[0] != 0x0a {
		return nil
	}

	// Decode varint length
	length, bytesRead := decodeVarint(buf[1:])
	if bytesRead == 0 {
		return nil
	}

	// Check bounds
	dataStart := 1 + bytesRead
	if dataStart+length > len(buf) {
		return nil
	}

	return buf[dataStart : dataStart+length]
}

// encodeVarint encodes an integer as a protobuf varint
func encodeVarint(n int) []byte {
	var varint []byte
	for n > 0x7f {
		varint = append(varint, byte(n&0x7f)|0x80)
		n >>= 7
	}
	varint = append(varint, byte(n))
	return varint
}

// decodeVarint decodes a protobuf varint from the buffer.
// Returns (value, bytesRead). bytesRead is 0 if malformed.
func decodeVarint(buf []byte) (int, int) {
	var value int
	var shift uint
	var i int

	for i < len(buf) {
		b := buf[i]
		value |= int(b&0x7f) << shift
		i++

		if b&0x80 == 0 {
			return value, i
		}

		shift += 7
		if shift > 28 {
			return 0, 0 // overflow
		}
	}

	return 0, 0 // incomplete varint
}
