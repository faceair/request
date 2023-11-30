package request

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestBasicGzipReader(t *testing.T) {
	input := "这是一段用于测试的文本"
	reader := strings.NewReader(input)
	gr := newGzipReader(reader)
	defer gr.Close()

	var compressedData bytes.Buffer
	_, err := io.Copy(&compressedData, gr)
	if err != nil {
		t.Errorf("Failed to read compressed data: %v", err)
	}

	gz, err := gzip.NewReader(&compressedData)
	if err != nil {
		t.Errorf("Failed to create gzip reader: %v", err)
	}
	defer gz.Close()
	uncompressedData, err := io.ReadAll(gz)
	if err != nil {
		t.Errorf("Failed to read uncompressed data: %v", err)
	}

	if string(uncompressedData) != input {
		t.Errorf("Uncompressed data does not match original, got: %s, want: %s", string(uncompressedData), input)
	}
}

func TestGzipEOFBehavior(t *testing.T) {
	input := "简短的测试文本"
	reader := strings.NewReader(input)
	gr := newGzipReader(reader)
	defer gr.Close()

	_, err := io.Copy(io.Discard, gr)
	if err != nil {
		t.Fatalf("Failed to read until EOF: %v", err)
	}

	buf := make([]byte, 10)
	n, err := gr.Read(buf)
	if err != io.EOF {
		t.Errorf("Expected EOF, got error: %v", err)
	}
	if n != 0 {
		t.Errorf("Expected 0 bytes read after EOF, got %d", n)
	}
}

func TestGzipResetMethod(t *testing.T) {
	input1 := "第一个测试文本"
	input2 := "第二个不同的测试文本"
	reader1 := strings.NewReader(input1)
	reader2 := strings.NewReader(input2)

	gr := newGzipReader(reader1)
	defer gr.Close()

	io.Copy(io.Discard, gr)

	gr.Reset(reader2)

	var compressedData bytes.Buffer
	io.Copy(&compressedData, gr)

	gz, _ := gzip.NewReader(&compressedData)
	uncompressedData, _ := io.ReadAll(gz)
	if string(uncompressedData) != input2 {
		t.Errorf("After reset, uncompressed data does not match second input, got: %s, want: %s", string(uncompressedData), input2)
	}
}
