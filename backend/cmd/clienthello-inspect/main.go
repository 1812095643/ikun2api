package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	utls "github.com/refraction-networking/utls"
)

func inspect(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	spec, err := (&utls.Fingerprinter{AllowBluntMimicry: true}).RawClientHello(raw)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"cipher_suites": spec.CipherSuites, "tls_min": spec.TLSVersMin, "tls_max": spec.TLSVersMax}
	var extensions []uint16
	var names []string
	for _, extension := range spec.Extensions {
		if _, isSNI := extension.(*utls.SNIExtension); isSNI {
			extensions = append(extensions, 0)
			names = append(names, fmt.Sprintf("%T", extension))
			continue
		}
		buffer := make([]byte, extension.Len())
		length, readErr := extension.Read(buffer)
		if length < 4 {
			return nil, fmt.Errorf("extension %T: %w", extension, readErr)
		}
		extensions = append(extensions, binary.BigEndian.Uint16(buffer))
		names = append(names, fmt.Sprintf("%T", extension))
		switch value := extension.(type) {
		case *utls.SupportedCurvesExtension:
			groups := make([]uint16, len(value.Curves))
			for index, group := range value.Curves {
				groups[index] = uint16(group)
			}
			result["curves"] = groups
		case *utls.SupportedPointsExtension:
			points := make([]uint16, len(value.SupportedPoints))
			for index, point := range value.SupportedPoints {
				points[index] = uint16(point)
			}
			result["point_formats"] = points
		case *utls.SignatureAlgorithmsExtension:
			algorithms := make([]uint16, len(value.SupportedSignatureAlgorithms))
			for index, algorithm := range value.SupportedSignatureAlgorithms {
				algorithms[index] = uint16(algorithm)
			}
			result["signature_algorithms"] = algorithms
		case *utls.ALPNExtension:
			result["alpn_protocols"] = value.AlpnProtocols
		case *utls.SupportedVersionsExtension:
			result["supported_versions"] = value.Versions
		case *utls.KeyShareExtension:
			groups := make([]uint16, len(value.KeyShares))
			for index, share := range value.KeyShares {
				groups[index] = uint16(share.Group)
			}
			result["key_share_groups"] = groups
		case *utls.PSKKeyExchangeModesExtension:
			modes := make([]uint16, len(value.Modes))
			for index, mode := range value.Modes {
				modes[index] = uint16(mode)
			}
			result["psk_modes"] = modes
		case *utls.UtlsCompressCertExtension:
			algorithms := make([]uint16, len(value.Algorithms))
			for index, algorithm := range value.Algorithms {
				algorithms[index] = uint16(algorithm)
			}
			result["compress_cert_algos"] = algorithms
		}
	}
	result["extensions"] = extensions
	result["extension_types"] = names
	return result, nil
}

func main() {
	for _, path := range os.Args[1:] {
		result, err := inspect(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, path+": "+err.Error())
			os.Exit(1)
		}
		encoded, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(path+".json", append(encoded, '\n'), 0644); err != nil {
			panic(err)
		}
		fmt.Println(string(encoded))
	}
}
