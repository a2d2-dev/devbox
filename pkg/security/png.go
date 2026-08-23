package security

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
)

func encodePNG(img image.Image) (string, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b.Bytes()), nil
}
