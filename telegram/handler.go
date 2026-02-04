package telegram

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"encoding/base64"
	"io"
	"net/http"
	"tgproxy/types"
	"time"

	"github.com/zelenin/go-tdlib/client"
)

func (c *Client) StartMessageHandler(ctx context.Context) {
	c.log.Info("Message handler started, listening for updates...")

	listener := c.client.GetListener()
	defer listener.Close()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Message handler stopped")
			return

		case update := <-listener.Updates:
			if update.GetClass() == client.ClassUpdate {
				c.handleUpdate(update)
			}
		}
	}
}

func (c *Client) handleUpdate(update client.Type) {
	if newMsgUpdate, ok := update.(*client.UpdateNewMessage); ok {
		c.handleNewMessage(newMsgUpdate)
	}
}

func (c *Client) handleNewMessage(update *client.UpdateNewMessage) {
	msg := update.Message

	chatInfo, err := c.getChatInfo(msg.ChatId)
	if err != nil {
		c.log.Warn("Failed to get chat info for new message", "chat_id", msg.ChatId, "error", err)
		return
	}

	peerID := strconv.FormatInt(msg.ChatId, 10)
	peerName := chatInfo.Title

	contentType := msg.Content.MessageContentType()
	c.log.Info("New message received",
		"message_id", msg.Id,
		"chat_id", msg.ChatId,
		"type", contentType)

	convertedMsg, err := c.convertMessage(msg, peerID, peerName)
	if err != nil {
		c.log.Warn("Failed to convert new message", "msg_id", msg.Id, "error", err)
		return
	}

	select {
	case c.router.Inbound() <- convertedMsg:
		c.log.Debug("Message routed successfully", "msg_id", msg.Id)
	default:
		c.log.Warn("Router inbound channel full, dropping message", "msg_id", msg.Id)
	}
}

func (c *Client) SendMessage(message *types.Message) error {
	switch message.Type {
	case "text":
		return c.sendTextMessage(message)
	case "photo":
		return c.sendPhotoMessage(message)
	case "video":
		return c.sendVideoMessage(message)
	case "document":
		return c.sendDocumentMessage(message)
	case "sticker":
		return c.sendStickerMessage(message)
	case "voice":
		return c.sendVoiceMessage(message)
	case "video_note":
		return c.sendVideoNoteMessage(message)
	default:
		return fmt.Errorf("unsupported message type: %s", message.Type)
	}
}

func (c *Client) sendTextMessage(message *types.Message) error {
	textContent, ok := message.Content.(types.TextContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected TextContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	textPreview := textContent.Text
	if len(textPreview) > 100 {
		textPreview = textPreview[:100] + "..."
	}
	c.log.Info("Sending text message to chat",
		"chat_id", chatID,
		"text_preview", textPreview)

	formattedText := &client.FormattedText{
		Text: textContent.Text,
	}

	inputContent := &client.InputMessageText{
		Text: formattedText,
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send text message",
			"chat_id", chatID,
			"text_preview", textPreview,
			"error", err)
		return fmt.Errorf("failed to send message to chat %d: %w", chatID, err)
	}

	c.log.Info("Text message sent successfully",
		"chat_id", chatID,
		"text_preview", textPreview)
	return nil
}

func (c *Client) getFileRefAndMimeType(mediaURL string) (*client.File, string, error) {
	if strings.HasPrefix(mediaURL, "data:") {
		return c.dataUrlToTemp(mediaURL)
	}

	schemeParts := strings.SplitN(mediaURL, "://", 2)
	if len(schemeParts) != 2 {
		return nil, "", fmt.Errorf("invalid media URL format: expected scheme://chat_id/message_id or http(s)://... or data:...")
	}

	scheme := schemeParts[0]
	if scheme == "http" || scheme == "https" {
		return c.downloadHttpToTemp(mediaURL)
	}

	path := strings.Trim(strings.TrimSpace(schemeParts[1]), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid media URL format: expected scheme://chat_id/message_id")
	}

	chatID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid chat ID: %w", err)
	}

	msgID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid message ID: %w", err)
	}

	c.log.Info("Parsing attachment request",
		"media_url", mediaURL,
		"scheme", scheme,
		"chat_id", chatID,
		"msg_id", msgID)

	msg, err := c.client.GetMessage(&client.GetMessageRequest{
		ChatId:    chatID,
		MessageId: msgID,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to retrieve message %d in chat %d: %w", msgID, chatID, err)
	}
	if msg == nil {
		return nil, "", fmt.Errorf("message %d not found in chat %d", msgID, chatID)
	}

	c.log.Info("Message retrieved successfully for attachment",
		"chat_id", chatID,
		"msg_id", msgID,
		"content_type", msg.Content.MessageContentType())

	contentType := msg.Content.MessageContentType()
	var fileRef *client.File
	var mimeType string

	switch contentType {
	case client.TypeMessagePhoto:
		photoContent := msg.Content.(*client.MessagePhoto)
		if len(photoContent.Photo.Sizes) == 0 {
			return nil, "", fmt.Errorf("no photo sizes found in message %d", msgID)
		}
		// Use largest size
		fileRef = photoContent.Photo.Sizes[len(photoContent.Photo.Sizes)-1].Photo
		mimeType = "image/jpeg" // Default for photos; could refine based on sizes[0].Type
	case client.TypeMessageVideo:
		videoContent := msg.Content.(*client.MessageVideo)
		fileRef = videoContent.Video.Video
		mimeType = videoContent.Video.MimeType
	case client.TypeMessageDocument:
		docContent := msg.Content.(*client.MessageDocument)
		fileRef = docContent.Document.Document
		mimeType = docContent.Document.MimeType
	case client.TypeMessageSticker:
		stickerContent := msg.Content.(*client.MessageSticker)
		fileRef = stickerContent.Sticker.Sticker
		mimeType = "image/webp"
	case client.TypeMessageVoiceNote:
		voiceContent := msg.Content.(*client.MessageVoiceNote)
		fileRef = voiceContent.VoiceNote.Voice
		mimeType = voiceContent.VoiceNote.MimeType
	case client.TypeMessageVideoNote:
		videoNoteContent := msg.Content.(*client.MessageVideoNote)
		fileRef = videoNoteContent.VideoNote.Video
		mimeType = "video/mp4" // Common for video notes
	default:
		return nil, "", fmt.Errorf("unsupported attachment type %s in message %d", contentType, msgID)
	}

	if fileRef == nil || fileRef.Id == 0 {
		return nil, "", fmt.Errorf("no valid file reference in message %d", msgID)
	}

	c.log.Info("File extracted from message",
		"file_id", fileRef.Id,
		"mime_type", mimeType,
		"is_downloaded", fileRef.Local.IsDownloadingCompleted)

	if fileRef.Local.IsDownloadingCompleted {
		c.log.Info("File already downloaded", "path", fileRef.Local.Path)
	} else {
		c.log.Info("Starting file download", "file_id", fileRef.Id)

		_, err := c.client.DownloadFile(&client.DownloadFileRequest{
			FileId:   fileRef.Id,
			Priority: 1, // low priority
		})
		if err != nil {
			return nil, "", fmt.Errorf("failed to start download for file %d: %w", fileRef.Id, err)
		}

		// Poll for download completion (30s timeout)
		timeout := time.Now().Add(30 * time.Second)
		for time.Now().Before(timeout) {
			time.Sleep(200 * time.Millisecond)

			updatedFile, err := c.client.GetFile(&client.GetFileRequest{
				FileId: fileRef.Id,
			})
			if err != nil {
				c.log.Warn("Failed to get updated file status", "file_id", fileRef.Id, "error", err)
				continue
			}

			if updatedFile.Local.IsDownloadingCompleted {
				fileRef = updatedFile
				c.log.Info("File download completed", "path", fileRef.Local.Path)
				break
			}
		}

		if !fileRef.Local.IsDownloadingCompleted {
			return nil, "", fmt.Errorf("file %d download timed out after 30s", fileRef.Id)
		}
	}

	return fileRef, mimeType, nil
}

func (c *Client) GetAttachment(mediaURL string) ([]byte, string, error) {
	fileRef, mimeType, err := c.getFileRefAndMimeType(mediaURL)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(fileRef.Local.Path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read downloaded file %q: %w", fileRef.Local.Path, err)
	}

	c.log.Info("Attachment file read successfully",
		"path", fileRef.Local.Path,
		"size_bytes", len(data))

	return data, mimeType, nil
}

func (c *Client) downloadHttpToTemp(mediaURL string) (*client.File, string, error) {
	c.log.Debug("Downloading HTTP media", "url", mediaURL)

	resp, err := http.Get(mediaURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download %s: %w", mediaURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP status %d for %s", resp.StatusCode, mediaURL)
	}

	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}

	tmpf, err := os.CreateTemp("", "tgproxy-http-*")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpf.Close()

	_, err = io.Copy(tmpf, resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to save downloaded data: %w", err)
	}

	// Refine MIME type if generic
	if mime == "application/octet-stream" {
		data, readErr := os.ReadFile(tmpf.Name())
		if readErr == nil && len(data) > 0 {
			mime = http.DetectContentType(data)
		}
	}

	file := &client.File{
		Id: 0,
		Local: &client.LocalFile{
			Path:                     tmpf.Name(),
			IsDownloadingCompleted:   true,
		},
	}

	c.log.Debug("HTTP media downloaded", "path", file.Local.Path, "mime", mime)
	return file, mime, nil
}

func (c *Client) dataUrlToTemp(mediaURL string) (*client.File, string, error) {
	c.log.Debug("Processing base64 data URL", "url", mediaURL)

	if !strings.HasPrefix(mediaURL, "data:") {
		return nil, "", fmt.Errorf("not a data URL: %s", mediaURL)
	}

	commaIdx := strings.Index(mediaURL, ",")
	if commaIdx < 0 {
		return nil, "", fmt.Errorf("invalid data URL (no comma separator)")
	}

	header := mediaURL[5:commaIdx] // skip "data:"
	body := mediaURL[commaIdx+1:]

	mimeParts := strings.SplitN(header, ";", 2)
	mime := mimeParts[0]
	if mime == "" {
		mime = "application/octet-stream"
	}

	if !strings.Contains(header, "base64") {
		return nil, "", fmt.Errorf("non-base64 data URLs not supported")
	}

	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, "", fmt.Errorf("base64 decode failed: %w", err)
	}

	tmpf, err := os.CreateTemp("", "tgproxy-data-*")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpf.Close()

	_, err = tmpf.Write(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to write data: %w", err)
	}

	// Refine MIME if generic
	if mime == "application/octet-stream" {
		sniff := data
		if len(sniff) > 512 {
			sniff = sniff[:512]
		}
		mime = http.DetectContentType(sniff)
	}

	file := &client.File{
		Id: 0,
		Local: &client.LocalFile{
			Path:                     tmpf.Name(),
			IsDownloadingCompleted:   true,
		},
	}

	c.log.Debug("Data URL processed", "path", file.Local.Path, "mime", mime, "size", len(data))
	return file, mime, nil
}

func (c *Client) getInputFile(fileRef *client.File) client.InputFile {
	if fileRef.Id != 0 {
		return &client.InputFileId{Id: fileRef.Id}
	}
	return &client.InputFileLocal{Path: fileRef.Local.Path}
}

func (c *Client) sendPhotoMessage(message *types.Message) error {
	photoContent, ok := message.Content.(types.PhotoContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected PhotoContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	captionPreview := photoContent.Caption
	if len(captionPreview) > 100 {
		captionPreview = captionPreview[:100] + "..."
	}
	c.log.Info("Sending photo message to chat",
		"chat_id", chatID,
		"caption_preview", captionPreview,
		"media_url", photoContent.MediaURL)

	fileRef, _, err := c.getFileRefAndMimeType(photoContent.MediaURL)
	if err != nil {
		return fmt.Errorf("failed to get photo file: %w", err)
	}

	var captionFT *client.FormattedText
	if photoContent.Caption != "" {
		captionFT = &client.FormattedText{Text: photoContent.Caption}
	}

	inputContent := &client.InputMessagePhoto{
		Photo:   c.getInputFile(fileRef),
		Caption: captionFT,
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send photo message",
			"chat_id", chatID,
			"media_url", photoContent.MediaURL,
			"error", err)
		return fmt.Errorf("failed to send photo to chat %d: %w", chatID, err)
	}

	c.log.Info("Photo message sent successfully",
		"chat_id", chatID,
		"caption_preview", captionPreview)
	return nil
}

func (c *Client) sendVideoMessage(message *types.Message) error {
	videoContent, ok := message.Content.(types.VideoContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected VideoContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	captionPreview := videoContent.Caption
	if len(captionPreview) > 100 {
		captionPreview = captionPreview[:100] + "..."
	}
	c.log.Info("Sending video message to chat",
		"chat_id", chatID,
		"caption_preview", captionPreview,
		"media_url", videoContent.MediaURL,
		"duration", videoContent.Duration)

	fileRef, _, err := c.getFileRefAndMimeType(videoContent.MediaURL)
	if err != nil {
		return fmt.Errorf("failed to get video file: %w", err)
	}

	var captionFT *client.FormattedText
	if videoContent.Caption != "" {
		captionFT = &client.FormattedText{Text: videoContent.Caption}
	}

	inputContent := &client.InputMessageVideo{
		Video:   c.getInputFile(fileRef),
		Caption: captionFT,
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send video message",
			"chat_id", chatID,
			"media_url", videoContent.MediaURL,
			"error", err)
		return fmt.Errorf("failed to send video to chat %d: %w", chatID, err)
	}

	c.log.Info("Video message sent successfully",
		"chat_id", chatID,
		"caption_preview", captionPreview)
	return nil
}

func (c *Client) sendDocumentMessage(message *types.Message) error {
	docContent, ok := message.Content.(types.DocumentContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected DocumentContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	filenamePreview := docContent.Filename
	if filenamePreview == "" {
		filenamePreview = "document"
	}
	if len(filenamePreview) > 100 {
		filenamePreview = filenamePreview[:100] + "..."
	}
	c.log.Info("Sending document message to chat",
		"chat_id", chatID,
		"filename", filenamePreview,
		"media_url", docContent.MediaURL)

	fileRef, _, err := c.getFileRefAndMimeType(docContent.MediaURL)
	if err != nil {
		return fmt.Errorf("failed to get document file: %w", err)
	}

	inputContent := &client.InputMessageDocument{
		Document: c.getInputFile(fileRef),
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send document message",
			"chat_id", chatID,
			"media_url", docContent.MediaURL,
			"error", err)
		return fmt.Errorf("failed to send document to chat %d: %w", chatID, err)
	}

	c.log.Info("Document message sent successfully",
		"chat_id", chatID,
		"filename", filenamePreview)
	return nil
}

func (c *Client) sendStickerMessage(message *types.Message) error {
	stickerContent, ok := message.Content.(types.StickerContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected StickerContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	emojiPreview := stickerContent.Emoji
	if emojiPreview == "" {
		emojiPreview = "no emoji"
	}
	c.log.Info("Sending sticker message to chat",
		"chat_id", chatID,
		"emoji", emojiPreview,
		"media_url", stickerContent.MediaURL)

	fileRef, _, err := c.getFileRefAndMimeType(stickerContent.MediaURL)
	if err != nil {
		return fmt.Errorf("failed to get sticker file: %w", err)
	}

	inputContent := &client.InputMessageSticker{
		Sticker: c.getInputFile(fileRef),
		// Emoji: stickerContent.Emoji, // if field exists
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send sticker message",
			"chat_id", chatID,
			"media_url", stickerContent.MediaURL,
			"error", err)
		return fmt.Errorf("failed to send sticker to chat %d: %w", chatID, err)
	}

	c.log.Info("Sticker message sent successfully",
		"chat_id", chatID,
		"emoji", emojiPreview)
	return nil
}

func (c *Client) sendVoiceMessage(message *types.Message) error {
	voiceContent, ok := message.Content.(types.VoiceContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected VoiceContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	c.log.Info("Sending voice message to chat",
		"chat_id", chatID,
		"duration", voiceContent.Duration,
		"media_url", voiceContent.MediaURL)

	fileRef, _, err := c.getFileRefAndMimeType(voiceContent.MediaURL)
	if err != nil {
		return fmt.Errorf("failed to get voice file: %w", err)
	}

	inputContent := &client.InputMessageVoiceNote{
		VoiceNote: c.getInputFile(fileRef),
		Duration:  int32(voiceContent.Duration),
		Waveform:  []byte{}, // optional, skip detailed waveform
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send voice message",
			"chat_id", chatID,
			"media_url", voiceContent.MediaURL,
			"error", err)
		return fmt.Errorf("failed to send voice to chat %d: %w", chatID, err)
	}

	c.log.Info("Voice message sent successfully",
		"chat_id", chatID,
		"duration", voiceContent.Duration)
	return nil
}

func (c *Client) sendVideoNoteMessage(message *types.Message) error {
	videoNoteContent, ok := message.Content.(types.VideoNoteContent)
	if !ok {
		return fmt.Errorf("invalid content type: expected VideoNoteContent")
	}

	chatID, err := strconv.ParseInt(message.PeerID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", message.PeerID, err)
	}

	c.log.Info("Sending video note message to chat",
		"chat_id", chatID,
		"duration", videoNoteContent.Duration,
		"media_url", videoNoteContent.MediaURL)

	fileRef, _, err := c.getFileRefAndMimeType(videoNoteContent.MediaURL)
	if err != nil {
		return fmt.Errorf("failed to get video note file: %w", err)
	}

	inputContent := &client.InputMessageVideoNote{
		VideoNote: c.getInputFile(fileRef),
		Duration:  int32(videoNoteContent.Duration),
		Length:    0, // thumbnail length, optional
	}

	_, err = c.client.SendMessage(&client.SendMessageRequest{
		ChatId:              chatID,
		InputMessageContent: inputContent,
	})
	if err != nil {
		c.log.Error("Failed to send video note message",
			"chat_id", chatID,
			"media_url", videoNoteContent.MediaURL,
			"error", err)
		return fmt.Errorf("failed to send video note to chat %d: %w", chatID, err)
	}

	c.log.Info("Video note message sent successfully",
		"chat_id", chatID,
		"duration", videoNoteContent.Duration)
	return nil
}

func (c *Client) ResolveChatIDs(chats []string) ([]int64, error) {
	chatIDSet := make(map[int64]struct{})

	for _, chatStr := range chats {
		// Try to parse as chat ID first
		if id, err := strconv.ParseInt(chatStr, 10, 64); err == nil && id != 0 {
			chatIDSet[id] = struct{}{}
			continue
		}

		// Otherwise, search for chats
		chatIDs, err := c.fetchChatIDs(chatStr, 20)
		if err != nil {
			c.log.Warn("Failed to search chats", "query", chatStr, "error", err)
		} else {
			for _, id := range chatIDs {
				chatIDSet[id] = struct{}{}
			}
		}

		// Also search contacts (private chats use user_id as chat_id)
		userIDs, err := c.fetchContactUserIDs(chatStr, 20)
		if err != nil {
			c.log.Warn("Failed to search contacts", "query", chatStr, "error", err)
		} else {
			for _, uid := range userIDs {
				chatIDSet[uid] = struct{}{}
			}
		}
	}

	var uniqueChatIDs []int64
	for id := range chatIDSet {
		uniqueChatIDs = append(uniqueChatIDs, id)
	}

	// Sort descending by ID for consistency (messages will be sorted by timestamp later)
	sort.Slice(uniqueChatIDs, func(i, j int) bool {
		return uniqueChatIDs[i] > uniqueChatIDs[j]
	})

	return uniqueChatIDs, nil
}

func (c *Client) getMessagesForChat(chatID int64, msgLimit int) ([]*client.Message, error) {
	history, err := c.client.GetChatHistory(&client.GetChatHistoryRequest{
		ChatId:        chatID,
		Limit:         int32(msgLimit),
		FromMessageId: 0,
		Offset:        0,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get chat history for %d: %w", chatID, err)
	}
	// Messages are returned in reverse chronological order (newest first)
	return history.Messages, nil
}

func (c *Client) getChatInfo(chatID int64) (*client.Chat, error) {
	return c.client.GetChat(&client.GetChatRequest{ChatId: chatID})
}

func (c *Client) convertMessage(msg *client.Message, peerID string, peerName string) (types.Message, error) {
	m := types.Message{
		ID:        strconv.FormatInt(msg.Id, 10),
		PeerID:    peerID,
		PeerName:  peerName,
		Timestamp: int64(msg.Date),
	}

	// Sender basics (name TBD)
	senderID := ""
	switch sender := msg.SenderId.(type) {
	case *client.MessageSenderUser:
		senderID = strconv.FormatInt(sender.UserId, 10)
	case *client.MessageSenderChat:
		senderID = strconv.FormatInt(sender.ChatId, 10)
	}
	m.SenderID = senderID
	if msg.IsOutgoing {
		m.SenderName = "You"
	} else if senderID == peerID {
		m.SenderName = peerName
	}

	// Content
	contentType := msg.Content.MessageContentType()
	switch contentType {
	case client.TypeMessageText:
		textContent := msg.Content.(*client.MessageText)
		m.Type = "text"
		m.Content = types.TextContent{Text: textContent.Text.Text}
	case client.TypeMessagePhoto:
		photoContent := msg.Content.(*client.MessagePhoto)
		caption := ""
		if photoContent.Caption != nil {
			caption = photoContent.Caption.Text
		}
		m.Type = "photo"
		m.Content = types.PhotoContent{
			Caption:      caption,
			MediaURL:     fmt.Sprintf("photo://%d/%d", msg.ChatId, msg.Id),
			ThumbnailURL: fmt.Sprintf("thumb://%d/%d", msg.ChatId, msg.Id),
		}
	case client.TypeMessageVideo:
		videoContent := msg.Content.(*client.MessageVideo)
		caption := ""
		if videoContent.Caption != nil {
			caption = videoContent.Caption.Text
		}
		m.Type = "video"
		m.Content = types.VideoContent{
			Caption:      caption,
			MediaURL:     fmt.Sprintf("video://%d/%d", msg.ChatId, msg.Id),
			ThumbnailURL: fmt.Sprintf("thumb://%d/%d", msg.ChatId, msg.Id),
			Duration:     int(videoContent.Video.Duration),
			MimeType:     videoContent.Video.MimeType,
		}
	case client.TypeMessageDocument:
		docContent := msg.Content.(*client.MessageDocument)
		m.Type = "document"
		m.Content = types.DocumentContent{
			Filename: docContent.Document.FileName,
			MediaURL: fmt.Sprintf("document://%d/%d", msg.ChatId, msg.Id),
			MimeType: docContent.Document.MimeType,
			Size:     0,
		}
	case client.TypeMessageSticker:
		stickerContent := msg.Content.(*client.MessageSticker)
		m.Type = "sticker"
		m.Content = types.StickerContent{
			Emoji:    stickerContent.Sticker.Emoji,
			MediaURL: fmt.Sprintf("sticker://%d/%d", msg.ChatId, msg.Id),
		}
	case client.TypeMessageVoiceNote:
		voiceContent := msg.Content.(*client.MessageVoiceNote)
		m.Type = "voice"
		m.Content = types.VoiceContent{
			MediaURL: fmt.Sprintf("voice://%d/%d", msg.ChatId, msg.Id),
			Duration: int(voiceContent.VoiceNote.Duration),
			MimeType: voiceContent.VoiceNote.MimeType,
		}
	case client.TypeMessageVideoNote:
		videoNoteContent := msg.Content.(*client.MessageVideoNote)
		m.Type = "video_note"
		m.Content = types.VideoNoteContent{
			MediaURL:     fmt.Sprintf("video_note://%d/%d", msg.ChatId, msg.Id),
			ThumbnailURL: fmt.Sprintf("thumb://%d/%d", msg.ChatId, msg.Id),
			Duration:     int(videoNoteContent.VideoNote.Duration),
			MimeType:     "",
		}
	default:
		m.Type = string(contentType)
		m.Content = map[string]any{"raw_type": contentType}
	}

	return m, nil
}

func (c *Client) GetMessages(chats []string, limit int) (types.MessagesResponse, error) {
	c.log.Info("Getting messages from chats", "chats", chats, "limit_per_chat", limit)

	if len(chats) == 0 {
		return types.MessagesResponse{}, nil
	}

	chatIDs, err := c.ResolveChatIDs(chats)
	if err != nil {
		return types.MessagesResponse{}, fmt.Errorf("failed to resolve chat IDs: %w", err)
	}

	var allMessages []types.Message

	perChatLimit := limit
	if perChatLimit <= 0 {
		perChatLimit = 20 // default
	}

	for _, chatID := range chatIDs {
		chatInfo, err := c.getChatInfo(chatID)
		if err != nil {
			c.log.Warn("Failed to get chat info", "chat_id", chatID, "error", err)
			continue
		}

		peerID := strconv.FormatInt(chatID, 10)
		peerName := chatInfo.Title

		isPrivate := false
		if typ := chatInfo.Type.ChatTypeType(); typ == client.TypeChatTypePrivate {
			isPrivate = true
		}

		rawMessages, err := c.getMessagesForChat(chatID, perChatLimit)
		if err != nil {
			c.log.Warn("Failed to get messages for chat", "chat_id", chatID, "error", err)
			continue
		}

		for _, rawMsg := range rawMessages {
			if rawMsg == nil || (!isPrivate && rawMsg.IsOutgoing) {
				continue
			}

			converted, err := c.convertMessage(rawMsg, peerID, peerName)
			if err != nil {
				c.log.Warn("Failed to convert message", "msg_id", rawMsg.Id, "error", err)
				continue
			}

			allMessages = append(allMessages, converted)
		}
	}

	// Sort all messages by timestamp descending (newest first)
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp > allMessages[j].Timestamp
	})

	// Trim to total limit if specified
	if limit > 0 && len(allMessages) > limit {
		allMessages = allMessages[:limit]
	}

	return types.MessagesResponse{
		Messages: allMessages,
	}, nil
}

func (c *Client) fetchChatIDs(query string, chatLimit int32) ([]int64, error) {
	var chatIDs []int64
	if query == "" {
		chats, err := c.client.GetChats(&client.GetChatsRequest{Limit: chatLimit})
		if err != nil {
			c.log.Error("Failed to get chats", "error", err)
			return nil, err
		}
		chatIDs = chats.ChatIds
	} else {
		chats, err := c.client.SearchChatsOnServer(&client.SearchChatsOnServerRequest{
			Query: query,
			Limit: chatLimit,
		})
		if err != nil {
			c.log.Warn("Failed to search chats", "query", query, "error", err)
		} else {
			chatIDs = chats.ChatIds
		}
	}
	return chatIDs, nil
}

func (c *Client) fetchContactUserIDs(query string, contactsLimit int32) ([]int64, error) {
	contactsResp, err := c.client.SearchContacts(&client.SearchContactsRequest{
		Query: query,
		Limit: contactsLimit,
	})
	if err != nil {
		c.log.Warn("Failed to search contacts", "query", query, "error", err)
		return nil, nil
	}
	return contactsResp.UserIds, nil
}

func (c *Client) processChat(chatID int64) (types.Peer, bool, error) {
	chat, err := c.client.GetChat(&client.GetChatRequest{ChatId: chatID})
	if err != nil {
		return types.Peer{}, false, err
	}

	peerID := strconv.FormatInt(chatID, 10)
	lastTS := int64(0)
	if chat.LastMessage != nil {
		lastTS = int64(chat.LastMessage.Date)
	}

	name := chat.Title
	peerType := ""

	typ := chat.Type.ChatTypeType()
	switch typ {
	case client.TypeChatTypePrivate:
		peerType = "user"
	case client.TypeChatTypeBasicGroup:
		peerType = "group"
	case client.TypeChatTypeSupergroup:
		peerType = "supergroup"
	case client.TypeChatTypeSecret:
		return types.Peer{}, false, nil
	default:
		return types.Peer{}, false, nil
	}

	return types.Peer{
		PeerID:               peerID,
		Type:                 peerType,
		Name:                 name,
		LastMessageTimestamp: lastTS,
	}, true, nil
}

func (c *Client) processContact(userID int64) (types.Peer, error) {
	user, err := c.client.GetUser(&client.GetUserRequest{UserId: userID})
	if err != nil {
		return types.Peer{}, err
	}
	name := user.FirstName
	if user.LastName != "" {
		name += " " + user.LastName
	}
	peerIDStr := strconv.FormatInt(userID, 10)
	return types.Peer{
		PeerID:               peerIDStr,
		Type:                 "user",
		Name:                 name,
		LastMessageTimestamp: 0,
	}, nil
}

func (c *Client) GetPeers(query string, limit int) ([]types.Peer, error) {
	c.log.Info("Fetching peers from Telegram", "query", query, "limit", limit)

	peersMap := make(map[string]types.Peer)

	chatLimit := int32(100)
	if query != "" {
		chatLimit = 50
	}

	chatIDs, err := c.fetchChatIDs(query, chatLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to get chats: %w", err)
	}

	for _, chatID := range chatIDs {
		peer, ok, perr := c.processChat(chatID)
		if perr != nil {
			c.log.Warn("Failed to get chat details", "chat_id", chatID, "error", perr)
			continue
		}
		if !ok {
			continue
		}
		peersMap[peer.PeerID] = peer
	}

	contactsLimit := chatLimit
	contactUserIDs, _ := c.fetchContactUserIDs(query, contactsLimit)
	for _, userID := range contactUserIDs {
		peerIDStr := strconv.FormatInt(userID, 10)
		if _, exists := peersMap[peerIDStr]; exists {
			continue
		}
		peer, err := c.processContact(userID)
		if err != nil {
			c.log.Warn("Failed to get contact user", "user_id", userID, "error", err)
			continue
		}
		peersMap[peer.PeerID] = peer
	}

	var peers []types.Peer
	for _, peer := range peersMap {
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].LastMessageTimestamp > peers[j].LastMessageTimestamp
	})

	if limit > 0 && len(peers) > limit {
		peers = peers[:limit]
	}

	c.log.Info("Peers fetched from Telegram", "count", len(peers))

	return peers, nil
}
