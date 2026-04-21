package chat

import "strings"

// Canonical chat message types (JSON + DB). Legacy uppercase values are
// accepted on input and normalized on output.
const (
	MsgTypeText      = "text"
	MsgTypeImg       = "img"
	MsgTypeAudio     = "audio"
	MsgTypeVideo     = "video"
	MsgTypeVideoNote = "video_note"
	MsgTypeLocation  = "location"
)

// NormalizeMessageType maps client/legacy spellings to a canonical lowercase type.
// Unknown non-empty values are lowercased; empty returns empty.
func NormalizeMessageType(t string) string {
	s := strings.TrimSpace(t)
	if s == "" {
		return ""
	}
	u := strings.ToUpper(s)
	switch u {
	case "TEXT", "TXT":
		return MsgTypeText
	case "PHOTO", "IMAGE", "IMG", "PICTURE":
		return MsgTypeImg
	case "VOICE", "AUDIO", "SOUND", "MP3":
		return MsgTypeAudio
	case "VIDEO":
		return MsgTypeVideo
	case "VIDEO_NOTE", "VIDEONOTE", "ROUND", "ROUNDVIDEO", "CIRCLE":
		return MsgTypeVideoNote
	case "LOCATION":
		return MsgTypeLocation
	default:
		lo := strings.ToLower(s)
		switch lo {
		case "txt":
			return MsgTypeText
		case "photo", "image", "picture":
			return MsgTypeImg
		case "voice", "sound", "mp3":
			return MsgTypeAudio
		case "videonote", "round", "roundvideo", "circle":
			return MsgTypeVideoNote
		}
		return lo
	}
}

// MessageKindsEqual compares message/attachment/media kinds ignoring legacy casing.
func MessageKindsEqual(a, b string) bool {
	na, nb := NormalizeMessageType(a), NormalizeMessageType(b)
	if na == "" || nb == "" {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return na == nb
}

// CanonicalMessageTypeForAPI returns the normalized type for JSON responses
// (maps legacy DB rows like TEXT → text).
func CanonicalMessageTypeForAPI(t string) string {
	n := NormalizeMessageType(t)
	if n != "" {
		return n
	}
	return strings.ToLower(strings.TrimSpace(t))
}
