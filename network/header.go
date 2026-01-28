package network

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ActionFileSystem      = "filesystem"
	ActionFileSystemChunk = "filesystem-chunk"
	ActionFileChunk       = "file-chunk"
	ActionBegin           = "begin"
	ActionComplete        = "complete"
)

type DatboxHeader struct {
	Action string
	Fields map[string]string
}

func NewHeader(action string) *DatboxHeader {
	header := new(DatboxHeader)
	header.Action = action
	header.Fields = map[string]string{}
	return header
}

func ParseHeader(str string) (*DatboxHeader, error) {
	split := strings.Split(str, "\n")
	if len(split) == 0 {
		return nil, errors.New("Header string is empty")
	}
	header := NewHeader(split[0])
	for _, line := range split[1:] {
		innerSplit := strings.Split(line, ":")
		if len(innerSplit) <= 1 {
			return nil, errors.New("Header field is malformed: " + line)
		}
		header.Fields[innerSplit[0]] = strings.TrimSpace(strings.Join(innerSplit[1:], ":"))
	}
	return header, nil
}

func (h *DatboxHeader) String() string {
	builder := strings.Builder{}
	if h.Action == "" {
		builder.WriteString("unknown")
	} else {
		builder.WriteString(h.Action)
	}
	builder.WriteString("\n")
	for key, val := range h.Fields {
		builder.WriteString(fmt.Sprintf("%s: %s", key, val))
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}
