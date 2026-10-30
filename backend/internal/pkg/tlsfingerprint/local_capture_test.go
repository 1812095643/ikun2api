package tlsfingerprint

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/MACOS-DO/sub4api/internal/pkg/claude"
	"io"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

// 参考包来自服务器运行官方客户端的真实回环采集，不由待测模板生成。
func TestLocalDefaultMatchesNativeClientHello(t *testing.T) {
	reference, err := os.ReadFile("testdata/" + claude.EffectiveClientProfile().TLSSampleID + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	actual := captureLocalDefaultClientHello(t)
	parser := &utls.Fingerprinter{AllowBluntMimicry: true}
	want, err := parser.RawClientHello(reference)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parser.RawClientHello(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.CipherSuites, want.CipherSuites) {
		t.Fatalf("cipher suites differ from native capture: got %v, want %v", got.CipherSuites, want.CipherSuites)
	}
	if !bytes.Equal(got.CompressionMethods, want.CompressionMethods) {
		t.Fatal("compression methods differ from native capture")
	}
	if len(got.Extensions) != len(want.Extensions) {
		t.Fatalf("extension count differs: got %d, want %d", len(got.Extensions), len(want.Extensions))
	}
	for index := range want.Extensions {
		wantBytes := fixedExtensionFingerprint(t, want.Extensions[index])
		gotBytes := fixedExtensionFingerprint(t, got.Extensions[index])
		if !bytes.Equal(gotBytes, wantBytes) {
			t.Errorf("extension %d differs: got %T %x, want %T %x", index, got.Extensions[index], gotBytes, want.Extensions[index], wantBytes)
		}
	}
	if !bytes.Equal(actual[1:3], reference[1:3]) || !bytes.Equal(actual[9:11], reference[9:11]) {
		t.Error("record or ClientHello legacy version differs from native capture")
	}
}

func fixedExtensionFingerprint(t *testing.T, extension utls.TLSExtension) []byte {
	t.Helper()
	switch value := extension.(type) {
	case *utls.SNIExtension:
		return []byte{0, 0}
	case *utls.KeyShareExtension:
		// 密钥材料必须由 uTLS 每次重新生成，只比较组和排列，不复用采集包的公钥。
		result := []byte{0, 51}
		for _, share := range value.KeyShares {
			result = binary.BigEndian.AppendUint16(result, uint16(share.Group))
		}
		return result
	default:
		result := make([]byte, extension.Len())
		n, err := extension.Read(result)
		if n < 4 {
			t.Fatalf("cannot serialize extension %T: %v", extension, err)
		}
		return result[:n]
	}
}

func captureLocalDefaultClientHello(t *testing.T) []byte {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type capture struct {
		packet []byte
		err    error
	}
	result := make(chan capture, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			result <- capture{err: err}
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 5)
		if _, err := io.ReadFull(connection, header); err != nil {
			result <- capture{err: err}
			return
		}
		payload := make([]byte, binary.BigEndian.Uint16(header[3:5]))
		_, err = io.ReadFull(connection, payload)
		result <- capture{packet: append(header, payload...), err: err}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	port := listener.Addr().(*net.TCPAddr).Port
	connection, err := NewDialer(&Profile{Name: "local-default"}, nil).DialTLSContext(ctx, "tcp", fmt.Sprintf("localhost:%d", port))
	if connection != nil {
		connection.Close()
	}
	if err == nil {
		t.Fatal("capture receiver must stop before TLS authentication completes")
	}
	select {
	case observed := <-result:
		if observed.err != nil {
			t.Fatalf("capture failed: %v; dial: %v", observed.err, err)
		}
		return observed.packet
	case <-ctx.Done():
		t.Fatalf("ClientHello was not emitted: %v", err)
		return nil
	}
}
