package cargo

import (
	"testing"

	"github.com/google/uuid"
)

func TestParsePendingCargoPhotoRef(t *testing.T) {
	id := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	cases := []struct {
		in      string
		want    uuid.UUID
		wantOK  bool
	}{
		{"", uuid.Nil, false},
		{id.String(), id, true},
		{"  " + id.String() + "  ", id, true},
		{"https://app.example/api/cargo/photos/" + id.String(), id, true},
		{"/api/cargo/photos/" + id.String(), id, true},
		{"/api/cargo/photos/" + id.String() + "/", id, true},
		{"https://cdn.example.com/x.jpg", uuid.Nil, false},
	}
	for _, tc := range cases {
		got, ok := ParsePendingCargoPhotoRef(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("ParsePendingCargoPhotoRef(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}
