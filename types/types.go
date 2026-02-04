package types

import (
	"encoding/json"
	"fmt"
)

// SendMessage endpoint
type SendRequest struct {
	Contact string `json:"contact"`
	Text    string `json:"text"`
}

type SendResponse struct {
	Success   bool   `json:"success"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Peers response
type PeersResponse struct {
	Peers []Peer `json:"peers"`
}

type Peer struct {
	PeerID               string `json:"peer_id"`
	Type                 string `json:"type"`
	Name                 string `json:"name"`
	LastMessageTimestamp int64  `json:"last_message_timestamp"`
}

// Messages response
type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

type Message struct {
	ID         string `json:"id"`
	PeerID     string `json:"peer_id"`
	PeerName   string `json:"peer_name"`
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Timestamp  int64  `json:"timestamp"`
	Type       string `json:"type"`
	Content    any    `json:"content"`
}

// Text content
type TextContent struct {
	Text string `json:"text"`
}

// Photo content
type PhotoContent struct {
	Caption      string `json:"caption,omitempty"`
	MediaURL     string `json:"media_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// Video content
type VideoContent struct {
	Caption      string `json:"caption,omitempty"`
	MediaURL     string `json:"media_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
}

// Document content
type DocumentContent struct {
	Filename string `json:"filename,omitempty"`
	MediaURL string `json:"media_url"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Voice content
type VoiceContent struct {
	MediaURL string `json:"media_url"`
	Duration int    `json:"duration,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// Video Note
type VideoNoteContent struct {
	MediaURL     string `json:"media_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
}

// Sticker content
type StickerContent struct {
	Emoji    string `json:"emoji,omitempty"`
	MediaURL string `json:"media_url"`
}

// Media group content
type MediaGroupContent struct {
	Caption string      `json:"caption,omitempty"`
	Items   []MediaItem `json:"items"`
}

type MediaItem struct {
	MessageID    string `json:"message_id"`
	Type         string `json:"type"`
	MediaURL     string `json:"media_url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
}

// Error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// Custom UnmarshalJSON for Message
func UnmarshalJSON(data []byte) (*Message, error) {
	m := Message{}

	// Temporary struct to capture all fields except Content
	type Alias Message
	aux := struct {
		*Alias
		Content json.RawMessage `json:"content"`
	}{
		Alias: (*Alias)(&m),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return &m, err
	}

	// Now handle Content based on Type
	switch m.Type {
	case "text":
		var text TextContent
		if err := json.Unmarshal(aux.Content, &text); err != nil {
			return nil, fmt.Errorf("failed to unmarshal text content: %w", err)
		}
		m.Content = text
	case "photo":
		var photo PhotoContent
		if err := json.Unmarshal(aux.Content, &photo); err != nil {
			return nil, fmt.Errorf("failed to unmarshal photo content: %w", err)
		}
		m.Content = photo
	case "video":
		var video VideoContent
		if err := json.Unmarshal(aux.Content, &video); err != nil {
			return nil, fmt.Errorf("failed to unmarshal video content: %w", err)
		}
		m.Content = video
	case "document":
		var document DocumentContent
		if err := json.Unmarshal(aux.Content, &document); err != nil {
			return nil, fmt.Errorf("failed to unmarshal document content: %w", err)
		}
		m.Content = document
	case "voice":
		var voice VoiceContent
		if err := json.Unmarshal(aux.Content, &voice); err != nil {
			return nil, fmt.Errorf("failed to unmarshal voice content: %w", err)
		}
		m.Content = voice
	case "video_note":
		var videoNote VideoNoteContent
		if err := json.Unmarshal(aux.Content, &videoNote); err != nil {
			return nil, fmt.Errorf("failed to unmarshal video note content: %w", err)
		}
		m.Content = videoNote
	case "sticker":
		var sticker StickerContent
		if err := json.Unmarshal(aux.Content, &sticker); err != nil {
			return nil, fmt.Errorf("failed to unmarshal sticker content: %w", err)
		}
		m.Content = sticker
	case "media_group":
		var mediaGroup MediaGroupContent
		if err := json.Unmarshal(aux.Content, &mediaGroup); err != nil {
			return nil, fmt.Errorf("failed to unmarshal media group content: %w", err)
		}
		m.Content = mediaGroup
	default:
		// For unknown types, store as raw map (or handle as needed)
		var raw map[string]any
		if err := json.Unmarshal(aux.Content, &raw); err != nil {
			return nil, fmt.Errorf("failed to unmarshal unknown content: %w", err)
		}
		m.Content = raw
	}

	return &m, nil
}
