package service

// javascriptUTF16CodeUnitAt uses JavaScript string indexing, shared by request
// compatibility code. It does not change the UTF-8 encoding of JSON on the wire.
func javascriptUTF16CodeUnitAt(text string, index int) (uint16, bool) {
	if index < 0 {
		return 0, false
	}
	position := 0
	for _, r := range text {
		if r <= 0xFFFF {
			if position == index {
				return uint16(r), true
			}
			position++
			continue
		}
		value := r - 0x10000
		high := uint16(0xD800 + (value >> 10))
		low := uint16(0xDC00 + (value & 0x3FF))
		if position == index {
			return high, true
		}
		position++
		if position == index {
			return low, true
		}
		position++
	}
	return 0, false
}

// javascriptUTF16Prefix returns the longest prefix fitting maxUnits UTF-16 code
// units. Unlike String.slice, it never leaves half of a surrogate pair: JSON
// encoders would replace that half and silently change the text. Slice only at
// Go range boundaries so the returned prefix also remains valid UTF-8.
func javascriptUTF16Prefix(text string, maxUnits int) (string, bool) {
	// For valid UTF-8, its byte length bounds the UTF-16 length from above.
	if len(text) <= maxUnits {
		return text, false
	}
	units := 0
	for byteIndex, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if width > maxUnits-units {
			return text[:byteIndex], true
		}
		units += width
	}
	return text, false
}
