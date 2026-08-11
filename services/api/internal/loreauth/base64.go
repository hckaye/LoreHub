package loreauth

import "strings"

func base64Raw(value []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var builder strings.Builder
	for index := 0; index < len(value); index += 3 {
		chunk := uint(value[index]) << 16
		remaining := len(value) - index
		if remaining > 1 {
			chunk |= uint(value[index+1]) << 8
		}
		if remaining > 2 {
			chunk |= uint(value[index+2])
		}
		builder.WriteByte(alphabet[(chunk>>18)&63])
		builder.WriteByte(alphabet[(chunk>>12)&63])
		if remaining > 1 {
			builder.WriteByte(alphabet[(chunk>>6)&63])
		}
		if remaining > 2 {
			builder.WriteByte(alphabet[chunk&63])
		}
	}
	return builder.String()
}
