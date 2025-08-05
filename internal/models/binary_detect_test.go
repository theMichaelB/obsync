package models_test

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
	
	"github.com/TheMichaelB/obsync/internal/models"
)

func TestIsBinaryFile(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content []byte
		want    bool
	}{
		{
			name: "text file by extension",
			path: "notes/test.md",
			content: []byte("# Hello World"),
			want: false,
		},
		{
			name: "binary file by extension",
			path: "images/photo.jpg",
			content: []byte{0xFF, 0xD8, 0xFF},
			want: true,
		},
		{
			name: "text file with unknown extension",
			path: "file.xyz",
			content: []byte("This is plain text"),
			want: false,
		},
		{
			name: "binary content with null bytes",
			path: "file.xyz",
			content: []byte{0x00, 0x01, 0x02, 0x03},
			want: true,
		},
		{
			name: "binary content with high non-printable ratio",
			path: "file.xyz",
			content: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			want: true,
		},
		{
			name: "empty file",
			path: "empty.txt",
			content: []byte{},
			want: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := models.IsBinaryFile(tt.path, tt.content)
			assert.Equal(t, tt.want, got)
		})
	}
}