// Package redis provides a low-level Redis client implementation using the RESP protocol.
// This is a custom implementation with zero external dependencies.
package redis

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// RESP (REdis Serialization Protocol) type prefixes
const (
	SimpleString = '+'
	Error        = '-'
	Integer      = ':'
	BulkString   = '$'
	Array        = '*'
)

var (
	ErrInvalidResp    = errors.New("invalid RESP format")
	ErrNilResponse    = errors.New("nil response")
	ErrUnexpectedType = errors.New("unexpected response type")
)

// Value represents a RESP value that can be any of the RESP types
type Value struct {
	Type    byte
	Str     string
	Int     int64
	Array   []Value
	IsNull  bool
	IsError bool
}

// String returns the string representation of the value
func (v Value) String() string {
	switch v.Type {
	case SimpleString, BulkString, Error:
		return v.Str
	case Integer:
		return strconv.FormatInt(v.Int, 10)
	case Array:
		return fmt.Sprintf("%v", v.Array)
	default:
		return ""
	}
}

// AsString returns the value as a string
func (v Value) AsString() (string, error) {
	if v.IsNull {
		return "", ErrNilResponse
	}
	if v.Type == SimpleString || v.Type == BulkString {
		return v.Str, nil
	}
	if v.Type == Integer {
		return strconv.FormatInt(v.Int, 10), nil
	}
	return "", ErrUnexpectedType
}

// AsInt returns the value as an integer
func (v Value) AsInt() (int64, error) {
	if v.IsNull {
		return 0, ErrNilResponse
	}
	if v.Type == Integer {
		return v.Int, nil
	}
	if v.Type == BulkString || v.Type == SimpleString {
		return strconv.ParseInt(v.Str, 10, 64)
	}
	return 0, ErrUnexpectedType
}

// AsArray returns the value as an array
func (v Value) AsArray() ([]Value, error) {
	if v.IsNull {
		return nil, ErrNilResponse
	}
	if v.Type == Array {
		return v.Array, nil
	}
	return nil, ErrUnexpectedType
}

// AsStringSlice converts an array value to a string slice
func (v Value) AsStringSlice() ([]string, error) {
	arr, err := v.AsArray()
	if err != nil {
		return nil, err
	}
	result := make([]string, len(arr))
	for i, item := range arr {
		s, err := item.AsString()
		if err != nil {
			return nil, err
		}
		result[i] = s
	}
	return result, nil
}

// AsMap converts an array value (field-value pairs) to a map
func (v Value) AsMap() (map[string]string, error) {
	arr, err := v.AsArray()
	if err != nil {
		return nil, err
	}
	if len(arr)%2 != 0 {
		return nil, errors.New("array length must be even for map conversion")
	}
	result := make(map[string]string, len(arr)/2)
	for i := 0; i < len(arr); i += 2 {
		key, err := arr[i].AsString()
		if err != nil {
			return nil, err
		}
		val, err := arr[i+1].AsString()
		if err != nil {
			return nil, err
		}
		result[key] = val
	}
	return result, nil
}

// Writer handles serializing commands to RESP format
type Writer struct {
	w *bufio.Writer
}

// NewWriter creates a new RESP writer
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: bufio.NewWriter(w)}
}

// WriteCommand writes a Redis command in RESP format
// Example: WriteCommand("SET", "key", "value") writes *3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n
func (w *Writer) WriteCommand(args ...string) error {
	// Write array header: *<count>\r\n
	if _, err := fmt.Fprintf(w.w, "*%d\r\n", len(args)); err != nil {
		return err
	}

	// Write each argument as bulk string: $<len>\r\n<data>\r\n
	for _, arg := range args {
		if _, err := fmt.Fprintf(w.w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}

	return w.w.Flush()
}

// Reader handles parsing RESP responses
type Reader struct {
	r *bufio.Reader
}

// NewReader creates a new RESP reader
func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r)}
}

// ReadValue reads a single RESP value from the stream
func (r *Reader) ReadValue() (Value, error) {
	// Read the type byte
	typeByte, err := r.r.ReadByte()
	if err != nil {
		return Value{}, err
	}

	switch typeByte {
	case SimpleString:
		return r.readSimpleString()
	case Error:
		return r.readError()
	case Integer:
		return r.readInteger()
	case BulkString:
		return r.readBulkString()
	case Array:
		return r.readArray()
	default:
		return Value{}, fmt.Errorf("%w: unknown type byte '%c'", ErrInvalidResp, typeByte)
	}
}

// readLine reads until \r\n and returns the line without the terminator
func (r *Reader) readLine() (string, error) {
	line, err := r.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	// Remove \r\n
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", ErrInvalidResp
	}
	return line[:len(line)-2], nil
}

// readSimpleString reads a simple string: +OK\r\n
func (r *Reader) readSimpleString() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}
	return Value{Type: SimpleString, Str: line}, nil
}

// readError reads an error: -ERR message\r\n
func (r *Reader) readError() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}
	return Value{Type: Error, Str: line, IsError: true}, nil
}

// readInteger reads an integer: :1000\r\n
func (r *Reader) readInteger() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}
	n, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return Value{}, fmt.Errorf("%w: invalid integer", ErrInvalidResp)
	}
	return Value{Type: Integer, Int: n}, nil
}

// readBulkString reads a bulk string: $6\r\nfoobar\r\n or $-1\r\n for nil
func (r *Reader) readBulkString() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}

	length, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return Value{}, fmt.Errorf("%w: invalid bulk string length", ErrInvalidResp)
	}

	// Null bulk string
	if length == -1 {
		return Value{Type: BulkString, IsNull: true}, nil
	}

	// Read the data + \r\n
	data := make([]byte, length+2)
	_, err = io.ReadFull(r.r, data)
	if err != nil {
		return Value{}, err
	}

	// Verify \r\n terminator
	if data[length] != '\r' || data[length+1] != '\n' {
		return Value{}, ErrInvalidResp
	}

	return Value{Type: BulkString, Str: string(data[:length])}, nil
}

// readArray reads an array: *2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n or *-1\r\n for nil
func (r *Reader) readArray() (Value, error) {
	line, err := r.readLine()
	if err != nil {
		return Value{}, err
	}

	count, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return Value{}, fmt.Errorf("%w: invalid array count", ErrInvalidResp)
	}

	// Null array
	if count == -1 {
		return Value{Type: Array, IsNull: true}, nil
	}

	// Read each element
	elements := make([]Value, count)
	for i := int64(0); i < count; i++ {
		val, err := r.ReadValue()
		if err != nil {
			return Value{}, err
		}
		elements[i] = val
	}

	return Value{Type: Array, Array: elements}, nil
}
