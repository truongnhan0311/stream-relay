package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrStreamNotFound = errors.New("stream not found")
	ErrNoMessages     = errors.New("no messages")
)

// StreamEntry represents a single entry in a Redis Stream
type StreamEntry struct {
	// ID is the entry ID (e.g., "1609459200000-0")
	ID string

	// Fields contains the field-value pairs
	Fields map[string]string
}

// StreamResult represents the result of reading from streams
type StreamResult struct {
	// Stream is the stream key name
	Stream string

	// Entries are the messages in this stream
	Entries []StreamEntry
}

// StreamInfo contains information about a stream
type StreamInfo struct {
	Length          int64
	RadixTreeKeys   int64
	RadixTreeNodes  int64
	LastGeneratedID string
	FirstEntry      *StreamEntry
	LastEntry       *StreamEntry
}

// Stream provides Redis Streams operations
type Stream struct {
	client *Client
}

// NewStream creates a new Stream wrapper
func NewStream(client *Client) *Stream {
	return &Stream{client: client}
}

// XAdd adds an entry to a stream
// Returns the ID of the new entry
// Use "*" as ID for auto-generated ID
func (s *Stream) XAdd(ctx context.Context, key string, id string, fields map[string]string) (string, error) {
	args := []string{"XADD", key, id}
	for k, v := range fields {
		args = append(args, k, v)
	}

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return "", err
	}
	if resp.IsError {
		return "", fmt.Errorf("XADD failed: %s", resp.Str)
	}

	return resp.AsString()
}

// XAddWithMaxLen adds an entry with MAXLEN trimming
func (s *Stream) XAddWithMaxLen(ctx context.Context, key string, maxLen int64, approximate bool, id string, fields map[string]string) (string, error) {
	args := []string{"XADD", key, "MAXLEN"}
	if approximate {
		args = append(args, "~")
	}
	args = append(args, strconv.FormatInt(maxLen, 10), id)
	for k, v := range fields {
		args = append(args, k, v)
	}

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return "", err
	}
	if resp.IsError {
		return "", fmt.Errorf("XADD failed: %s", resp.Str)
	}

	return resp.AsString()
}

// XRead reads from one or more streams
// block: 0 = block forever, -1 = don't block, >0 = block for that many milliseconds
func (s *Stream) XRead(ctx context.Context, count int64, block time.Duration, streams map[string]string) ([]StreamResult, error) {
	args := []string{"XREAD"}

	if count > 0 {
		args = append(args, "COUNT", strconv.FormatInt(count, 10))
	}

	if block >= 0 {
		blockMs := block.Milliseconds()
		args = append(args, "BLOCK", strconv.FormatInt(blockMs, 10))
	}

	args = append(args, "STREAMS")

	// Add stream keys first, then IDs
	keys := make([]string, 0, len(streams))
	ids := make([]string, 0, len(streams))
	for k, id := range streams {
		keys = append(keys, k)
		ids = append(ids, id)
	}
	args = append(args, keys...)
	args = append(args, ids...)

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return nil, err
	}
	if resp.IsNull {
		return nil, nil // No messages (timeout on block)
	}
	if resp.IsError {
		return nil, fmt.Errorf("XREAD failed: %s", resp.Str)
	}

	return s.parseStreamResults(resp)
}

// XReadGroup reads from streams as part of a consumer group
func (s *Stream) XReadGroup(ctx context.Context, group, consumer string, count int64, block time.Duration, noAck bool, streams map[string]string) ([]StreamResult, error) {
	args := []string{"XREADGROUP", "GROUP", group, consumer}

	if count > 0 {
		args = append(args, "COUNT", strconv.FormatInt(count, 10))
	}

	if block >= 0 {
		args = append(args, "BLOCK", strconv.FormatInt(block.Milliseconds(), 10))
	}

	if noAck {
		args = append(args, "NOACK")
	}

	args = append(args, "STREAMS")

	keys := make([]string, 0, len(streams))
	ids := make([]string, 0, len(streams))
	for k, id := range streams {
		keys = append(keys, k)
		ids = append(ids, id)
	}
	args = append(args, keys...)
	args = append(args, ids...)

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return nil, err
	}
	if resp.IsNull {
		return nil, nil
	}
	if resp.IsError {
		return nil, fmt.Errorf("XREADGROUP failed: %s", resp.Str)
	}

	return s.parseStreamResults(resp)
}

// XRange reads a range of entries from a stream
// Use "-" for start from beginning, "+" for end at latest
func (s *Stream) XRange(ctx context.Context, key, start, end string, count int64) ([]StreamEntry, error) {
	args := []string{"XRANGE", key, start, end}
	if count > 0 {
		args = append(args, "COUNT", strconv.FormatInt(count, 10))
	}

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return nil, err
	}
	if resp.IsError {
		return nil, fmt.Errorf("XRANGE failed: %s", resp.Str)
	}

	return s.parseEntries(resp)
}

// XRevRange reads a range of entries in reverse order
func (s *Stream) XRevRange(ctx context.Context, key, end, start string, count int64) ([]StreamEntry, error) {
	args := []string{"XREVRANGE", key, end, start}
	if count > 0 {
		args = append(args, "COUNT", strconv.FormatInt(count, 10))
	}

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return nil, err
	}
	if resp.IsError {
		return nil, fmt.Errorf("XREVRANGE failed: %s", resp.Str)
	}

	return s.parseEntries(resp)
}

// XLen returns the number of entries in a stream
func (s *Stream) XLen(ctx context.Context, key string) (int64, error) {
	resp, err := s.client.Do(ctx, "XLEN", key)
	if err != nil {
		return 0, err
	}
	if resp.IsError {
		return 0, fmt.Errorf("XLEN failed: %s", resp.Str)
	}
	return resp.AsInt()
}

// XTrim trims the stream to a certain size
func (s *Stream) XTrim(ctx context.Context, key string, maxLen int64, approximate bool) (int64, error) {
	args := []string{"XTRIM", key, "MAXLEN"}
	if approximate {
		args = append(args, "~")
	}
	args = append(args, strconv.FormatInt(maxLen, 10))

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	if resp.IsError {
		return 0, fmt.Errorf("XTRIM failed: %s", resp.Str)
	}
	return resp.AsInt()
}

// XDel deletes entries from a stream
func (s *Stream) XDel(ctx context.Context, key string, ids ...string) (int64, error) {
	args := append([]string{"XDEL", key}, ids...)
	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	if resp.IsError {
		return 0, fmt.Errorf("XDEL failed: %s", resp.Str)
	}
	return resp.AsInt()
}

// XGroupCreate creates a consumer group
func (s *Stream) XGroupCreate(ctx context.Context, key, group, id string, mkStream bool) error {
	args := []string{"XGROUP", "CREATE", key, group, id}
	if mkStream {
		args = append(args, "MKSTREAM")
	}

	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return err
	}
	if resp.IsError {
		// Ignore "BUSYGROUP" error (group already exists)
		if strings.Contains(resp.Str, "BUSYGROUP") {
			return nil
		}
		return fmt.Errorf("XGROUP CREATE failed: %s", resp.Str)
	}
	return nil
}

// XGroupDestroy destroys a consumer group
func (s *Stream) XGroupDestroy(ctx context.Context, key, group string) (int64, error) {
	resp, err := s.client.Do(ctx, "XGROUP", "DESTROY", key, group)
	if err != nil {
		return 0, err
	}
	if resp.IsError {
		return 0, fmt.Errorf("XGROUP DESTROY failed: %s", resp.Str)
	}
	return resp.AsInt()
}

// XGroupSetID sets the last delivered ID of a consumer group
func (s *Stream) XGroupSetID(ctx context.Context, key, group, id string) error {
	resp, err := s.client.Do(ctx, "XGROUP", "SETID", key, group, id)
	if err != nil {
		return err
	}
	if resp.IsError {
		return fmt.Errorf("XGROUP SETID failed: %s", resp.Str)
	}
	return nil
}

// XAck acknowledges messages
func (s *Stream) XAck(ctx context.Context, key, group string, ids ...string) (int64, error) {
	args := append([]string{"XACK", key, group}, ids...)
	resp, err := s.client.Do(ctx, args...)
	if err != nil {
		return 0, err
	}
	if resp.IsError {
		return 0, fmt.Errorf("XACK failed: %s", resp.Str)
	}
	return resp.AsInt()
}

// XPending returns information about pending messages
func (s *Stream) XPending(ctx context.Context, key, group string) (int64, string, string, map[string]int64, error) {
	resp, err := s.client.Do(ctx, "XPENDING", key, group)
	if err != nil {
		return 0, "", "", nil, err
	}
	if resp.IsError {
		return 0, "", "", nil, fmt.Errorf("XPENDING failed: %s", resp.Str)
	}

	arr, err := resp.AsArray()
	if err != nil {
		return 0, "", "", nil, err
	}
	if len(arr) != 4 {
		return 0, "", "", nil, errors.New("unexpected XPENDING response format")
	}

	count, _ := arr[0].AsInt()
	minID, _ := arr[1].AsString()
	maxID, _ := arr[2].AsString()

	consumers := make(map[string]int64)
	if !arr[3].IsNull {
		consumerArr, _ := arr[3].AsArray()
		for _, c := range consumerArr {
			cArr, _ := c.AsArray()
			if len(cArr) == 2 {
				name, _ := cArr[0].AsString()
				pending, _ := cArr[1].AsInt()
				consumers[name] = pending
			}
		}
	}

	return count, minID, maxID, consumers, nil
}

// XInfo returns information about a stream
func (s *Stream) XInfo(ctx context.Context, key string) (*StreamInfo, error) {
	resp, err := s.client.Do(ctx, "XINFO", "STREAM", key)
	if err != nil {
		return nil, err
	}
	if resp.IsError {
		return nil, fmt.Errorf("XINFO STREAM failed: %s", resp.Str)
	}

	arr, err := resp.AsArray()
	if err != nil {
		return nil, err
	}

	info := &StreamInfo{}
	for i := 0; i < len(arr)-1; i += 2 {
		key, _ := arr[i].AsString()
		switch key {
		case "length":
			info.Length, _ = arr[i+1].AsInt()
		case "radix-tree-keys":
			info.RadixTreeKeys, _ = arr[i+1].AsInt()
		case "radix-tree-nodes":
			info.RadixTreeNodes, _ = arr[i+1].AsInt()
		case "last-generated-id":
			info.LastGeneratedID, _ = arr[i+1].AsString()
		case "first-entry":
			if !arr[i+1].IsNull {
				entry, _ := s.parseSingleEntry(arr[i+1])
				info.FirstEntry = &entry
			}
		case "last-entry":
			if !arr[i+1].IsNull {
				entry, _ := s.parseSingleEntry(arr[i+1])
				info.LastEntry = &entry
			}
		}
	}

	return info, nil
}

// parseStreamResults parses XREAD/XREADGROUP response
func (s *Stream) parseStreamResults(resp Value) ([]StreamResult, error) {
	arr, err := resp.AsArray()
	if err != nil {
		return nil, err
	}

	results := make([]StreamResult, len(arr))
	for i, streamResp := range arr {
		streamArr, err := streamResp.AsArray()
		if err != nil || len(streamArr) != 2 {
			return nil, errors.New("invalid stream result format")
		}

		streamName, err := streamArr[0].AsString()
		if err != nil {
			return nil, err
		}

		entries, err := s.parseEntries(streamArr[1])
		if err != nil {
			return nil, err
		}

		results[i] = StreamResult{
			Stream:  streamName,
			Entries: entries,
		}
	}

	return results, nil
}

// parseEntries parses an array of stream entries
func (s *Stream) parseEntries(resp Value) ([]StreamEntry, error) {
	arr, err := resp.AsArray()
	if err != nil {
		return nil, err
	}

	entries := make([]StreamEntry, len(arr))
	for i, entryResp := range arr {
		entry, err := s.parseSingleEntry(entryResp)
		if err != nil {
			return nil, err
		}
		entries[i] = entry
	}

	return entries, nil
}

// parseSingleEntry parses a single stream entry [id, [field, value, ...]]
func (s *Stream) parseSingleEntry(resp Value) (StreamEntry, error) {
	arr, err := resp.AsArray()
	if err != nil || len(arr) != 2 {
		return StreamEntry{}, errors.New("invalid entry format")
	}

	id, err := arr[0].AsString()
	if err != nil {
		return StreamEntry{}, err
	}

	fields, err := arr[1].AsMap()
	if err != nil {
		return StreamEntry{}, err
	}

	return StreamEntry{
		ID:     id,
		Fields: fields,
	}, nil
}
