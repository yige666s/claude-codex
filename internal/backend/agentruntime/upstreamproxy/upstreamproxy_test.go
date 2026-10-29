package upstreamproxy

import "testing"

func TestEncodeDecodeChunk(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"medium", []byte("hello world, this is a test message")},
		{"large", make([]byte, 1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeChunk(tt.data)
			decoded := DecodeChunk(encoded)

			if decoded == nil {
				t.Fatal("DecodeChunk returned nil")
			}

			if len(decoded) != len(tt.data) {
				t.Errorf("length mismatch: got %d, want %d", len(decoded), len(tt.data))
			}

			for i := range tt.data {
				if decoded[i] != tt.data[i] {
					t.Errorf("data mismatch at index %d: got %d, want %d", i, decoded[i], tt.data[i])
					break
				}
			}
		})
	}
}

func TestDecodeChunkMalformed(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"wrong tag", []byte{0x0b, 0x05, 'h', 'e', 'l', 'l', 'o'}},
		{"incomplete", []byte{0x0a, 0x0a, 'h', 'e', 'l'}},
		{"empty", []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded := DecodeChunk(tt.data)
			if tt.name == "empty" {
				// Empty is valid (keepalive)
				if decoded == nil {
					t.Error("expected empty slice for empty input")
				}
			} else if tt.name == "wrong tag" || tt.name == "incomplete" {
				if decoded != nil {
					t.Error("expected nil for malformed input")
				}
			}
		})
	}
}

func TestEncodeVarint(t *testing.T) {
	tests := []struct {
		value    int
		expected []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xac, 0x02}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := encodeVarint(tt.value)
			if len(result) != len(tt.expected) {
				t.Errorf("length mismatch: got %d, want %d", len(result), len(tt.expected))
				return
			}
			for i := range tt.expected {
				if result[i] != tt.expected[i] {
					t.Errorf("byte mismatch at index %d: got 0x%02x, want 0x%02x", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestDecodeVarint(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		expectedVal  int
		expectedRead int
	}{
		{"zero", []byte{0x00}, 0, 1},
		{"one", []byte{0x01}, 1, 1},
		{"127", []byte{0x7f}, 127, 1},
		{"128", []byte{0x80, 0x01}, 128, 2},
		{"300", []byte{0xac, 0x02}, 300, 2},
		{"incomplete", []byte{0x80}, 0, 0},
		{"overflow", []byte{0x80, 0x80, 0x80, 0x80, 0x80}, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, read := decodeVarint(tt.data)
			if val != tt.expectedVal {
				t.Errorf("value mismatch: got %d, want %d", val, tt.expectedVal)
			}
			if read != tt.expectedRead {
				t.Errorf("read mismatch: got %d, want %d", read, tt.expectedRead)
			}
		})
	}
}

func TestGetProxyEnv(t *testing.T) {
	// Test when disabled
	globalState = &State{Enabled: false}
	env := GetProxyEnv()
	if len(env) != 0 {
		t.Error("expected empty env when disabled")
	}

	// Test when enabled
	globalState = &State{
		Enabled:      true,
		Port:         8080,
		CABundlePath: "/path/to/ca-bundle.crt",
	}
	env = GetProxyEnv()

	expectedKeys := []string{"HTTPS_PROXY", "https_proxy", "SSL_CERT_FILE", "NO_PROXY", "no_proxy"}
	for _, key := range expectedKeys {
		if _, ok := env[key]; !ok {
			t.Errorf("missing key: %s", key)
		}
	}

	if env["HTTPS_PROXY"] != "http://127.0.0.1:8080" {
		t.Errorf("unexpected HTTPS_PROXY: %s", env["HTTPS_PROXY"])
	}

	if env["SSL_CERT_FILE"] != "/path/to/ca-bundle.crt" {
		t.Errorf("unexpected SSL_CERT_FILE: %s", env["SSL_CERT_FILE"])
	}
}

func TestIsEnabled(t *testing.T) {
	globalState = &State{Enabled: false}
	if IsEnabled() {
		t.Error("expected disabled")
	}

	globalState = &State{Enabled: true}
	if !IsEnabled() {
		t.Error("expected enabled")
	}
}
