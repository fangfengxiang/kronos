package microv2

import (
	"encoding/binary"

	"google.golang.org/protobuf/encoding/protowire"
)

const maxResponseBytes = 1 << 20

type envelope struct {
	method   string
	sequence uint64
	failure  string
}

func readFrames(input []byte) (envelope, []byte, bool) {
	var header envelope
	encodedHeader, remainder, valid := splitFrame(input)
	if !valid {
		return header, nil, false
	}
	payload, trailer, valid := splitFrame(remainder)
	if !valid || len(trailer) != 0 {
		return header, nil, false
	}
	for len(encodedHeader) > 0 {
		field, kind, tagLength := protowire.ConsumeTag(encodedHeader)
		if tagLength < 0 {
			return header, nil, false
		}
		encodedHeader = encodedHeader[tagLength:]
		var valueLength int
		switch {
		case field == 1 && kind == protowire.BytesType:
			header.method, valueLength = protowire.ConsumeString(encodedHeader)
		case field == 2 && kind == protowire.Fixed64Type:
			header.sequence, valueLength = protowire.ConsumeFixed64(encodedHeader)
		case field == 3 && kind == protowire.BytesType:
			header.failure, valueLength = protowire.ConsumeString(encodedHeader)
		default:
			valueLength = protowire.ConsumeFieldValue(field, kind, encodedHeader)
		}
		if valueLength < 0 {
			return header, nil, false
		}
		encodedHeader = encodedHeader[valueLength:]
	}
	return header, payload, true
}

func splitFrame(input []byte) (frame, remainder []byte, valid bool) {
	if len(input) < 4 {
		return nil, nil, false
	}
	length := binary.BigEndian.Uint32(input[:4])
	input = input[4:]
	if uint64(length) > uint64(len(input)) {
		return nil, nil, false
	}
	return input[:length], input[length:], true
}

func encodeFrames(header envelope, payload, failure []byte, maxBytes int) []byte {
	if len(payload) > maxBytes || len(failure) > maxBytes || len(header.method) > maxBytes {
		return nil
	}
	encodedHeader := protowire.AppendTag(nil, 1, protowire.BytesType)
	encodedHeader = protowire.AppendString(encodedHeader, header.method)
	encodedHeader = protowire.AppendTag(encodedHeader, 2, protowire.Fixed64Type)
	encodedHeader = protowire.AppendFixed64(encodedHeader, header.sequence)
	if failure != nil {
		encodedHeader = protowire.AppendTag(encodedHeader, 3, protowire.BytesType)
		encodedHeader = protowire.AppendBytes(encodedHeader, failure)
	}
	const lengthPrefixesBytes = 2 * 4
	size := lengthPrefixesBytes + len(encodedHeader) + len(payload)
	if size > maxBytes {
		return nil
	}
	output := make([]byte, 0, size)
	output = binary.BigEndian.AppendUint32(output, uint32(len(encodedHeader)))
	output = append(output, encodedHeader...)
	output = binary.BigEndian.AppendUint32(output, uint32(len(payload)))
	return append(output, payload...)
}
