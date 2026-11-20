package bot

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/celestix/gotgproto/ext"
	"github.com/celestix/gotgproto/functions"
	tgstorage "github.com/celestix/gotgproto/storage"
	"github.com/dustin/go-humanize"
	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/html"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"

	"warpstash/internal/database"
	"warpstash/internal/util"
)

// replyHTML builds styled HTML text safely for gotgproto ctx.Reply without reader drain issues.
func replyHTML(s string) ext.ReplyTextType {
	return ext.ReplyTextStyledText(styling.Custom(func(eb *entity.Builder) error {
		if err := html.HTML(strings.NewReader(s), eb, html.Options{}); err != nil {
			eb.Plain(s)
		}
		return nil
	}))
}

// handleStart responds to the /start command.
func (b *BotService) handleStart(ctx *ext.Context, update *ext.Update) error {
	user := update.EffectiveUser()
	if user != nil && !b.cfg.IsTelegramUserAllowed(user.ID) {
		_, _ = ctx.Reply(update, replyHTML(
			"⛔ <b>Access Denied</b>\n\nYou are not authorized to use this Warpstash instance.",
		), nil)
		return nil
	}

	if user != nil && b.limiter != nil && !b.limiter.Allow(user.ID) {
		_, _ = ctx.Reply(update, replyHTML(
			"⏳ <b>Slow down!</b> You are sending requests too quickly. Please wait a moment.",
		), nil)
		return nil
	}

	welcomeText := fmt.Sprintf(
		"👋 <b>Welcome to Warpstash Bot!</b>\n\n"+
			"Send or forward any file (documents, videos, audio, images up to <b>%d MB</b>) to stash it directly on the server.\n\n"+
			"⏱ <b>Available Retentions:</b> <code>%s</code>\n"+
			"🔥 <b>Burn Option:</b> Auto-deletes immediately after the first download.\n\n"+
			"🔗 <b>Base URL:</b> <code>%s</code>",
		b.cfg.MaxFileSizeMB,
		strings.Join(b.cfg.AllowedExpiries, ", "),
		b.cfg.BaseURL,
	)

	_, err := ctx.Reply(update, replyHTML(welcomeText), nil)
	return err
}

// handleHelp responds to the /help command.
func (b *BotService) handleHelp(ctx *ext.Context, update *ext.Update) error {
	return b.handleStart(ctx, update)
}

// handleMedia processes incoming files/documents/photos/videos and offers inline expiration options.
func (b *BotService) handleMedia(ctx *ext.Context, update *ext.Update) error {
	user := update.EffectiveUser()
	if user != nil && !b.cfg.IsTelegramUserAllowed(user.ID) {
		_, _ = ctx.Reply(update, replyHTML(
			"⛔ <b>Access Denied:</b> You are not authorized to upload to this instance.",
		), nil)
		return nil
	}

	if user != nil && b.limiter != nil && !b.limiter.Allow(user.ID) {
		_, _ = ctx.Reply(update, replyHTML(
			"⏳ <b>Slow down!</b> You are uploading files too quickly. Please wait a moment before trying again.",
		), nil)
		return nil
	}

	msg := update.EffectiveMessage
	if msg == nil || msg.Media == nil {
		return nil
	}

	filename, fileSize, mimeType, err := b.extractMediaInfo(msg.Media)
	if err != nil {
		b.logger.Warn("unsupported media received", "error", err)
		return nil
	}

	// 1. Validate file size against config
	if fileSize > b.cfg.MaxFileSizeBytes() {
		errMsg := fmt.Sprintf(
			"❌ <b>File too large</b>\n\nFile size (<code>%s</code>) exceeds maximum limit of <b>%d MB</b>.",
			humanize.Bytes(uint64(fileSize)),
			b.cfg.MaxFileSizeMB,
		)
		_, _ = ctx.Reply(update, replyHTML(errMsg), nil)
		return nil
	}

	// 2. Pre-flight Physical Disk Space Check
	hasDiskSpace, err := b.storage.HasAvailableDiskSpace(fileSize)
	if err != nil {
		b.logger.Error("failed to query disk space", "error", err)
	} else if !hasDiskSpace {
		_, _ = ctx.Reply(update, replyHTML(
			"❌ <b>Server Storage Full</b>: Physical disk space is below safety reserve.",
		), nil)
		return nil
	}

	// 3. Pre-flight Application Quota Check
	if maxStorage := b.cfg.MaxTotalStorageBytes(); maxStorage > 0 {
		currentUsage, err := b.db.GetTotalStorageUsage(ctx)
		if err != nil {
			b.logger.Error("failed to query storage quota", "error", err)
		} else if currentUsage+fileSize > maxStorage {
			_, _ = ctx.Reply(update, replyHTML(
				"❌ <b>Storage Quota Exceeded</b>: Server total capacity reached.",
			), nil)
			return nil
		}
	}

	// Cache user peer with access hash in gotgproto storage
	if user != nil {
		ctx.PeerStorage.AddPeer(user.ID, user.AccessHash, tgstorage.TypeUser, user.Username)
	}
	inputPeer := update.EffectiveChat().GetInputPeer()

	// Store pending upload in cache (keyed by chatID:messageID)
	chatID := update.EffectiveChat().GetID()
	key := fmt.Sprintf("%d:%d", chatID, msg.ID)
	b.pending.Store(key, &PendingUpload{
		Media:     msg.Media,
		Filename:  filename,
		Size:      fileSize,
		MimeType:  mimeType,
		UserID:    user.ID,
		InputPeer: inputPeer,
		CreatedAt: time.Now(),
	})

	// Render prompt with inline buttons
	prompt := fmt.Sprintf(
		"📁 <b>File:</b> <code>%s</code>\n"+
			"📦 <b>Size:</b> <code>%s</code>\n\n"+
			"⏱ <i>Select expiration retention period:</i>",
		htmlEscape(filename),
		humanize.Bytes(uint64(fileSize)),
	)

	replyOpts := &ext.ReplyOpts{
		Markup: b.buildExpiryMarkup(msg.ID),
	}
	_, err = ctx.Reply(update, replyHTML(prompt), replyOpts)
	return err
}

// handleCallback handles inline button clicks for expiration selection.
func (b *BotService) handleCallback(ctx *ext.Context, update *ext.Update) error {
	cbq := update.CallbackQuery
	if cbq == nil {
		return nil
	}

	data := string(cbq.Data)
	if !strings.HasPrefix(data, "exp:") {
		return nil
	}

	parts := strings.Split(data, ":")
	if len(parts) != 3 {
		return nil
	}
	expiryChoice := parts[1]
	mediaMsgIDStr := parts[2]

	chatID := functions.GetChatIdFromPeer(cbq.Peer)
	userID := cbq.UserID

	if !b.cfg.IsTelegramUserAllowed(userID) {
		_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
			QueryID: cbq.QueryID,
			Message: "Access denied.",
			Alert:   true,
		})
		return nil
	}

	if b.limiter != nil && !b.limiter.Allow(userID) {
		_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
			QueryID: cbq.QueryID,
			Message: "⏳ Please wait a moment before tapping again.",
			Alert:   false,
		})
		return nil
	}

	// Immediate feedback to Telegram client
	_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
		QueryID: cbq.QueryID,
		Message: "Stashing file to Warpstash...",
	})

	key := fmt.Sprintf("%d:%s", chatID, mediaMsgIDStr)
	pending, ok := b.pending.Get(key)
	if !ok {
		peer := update.EffectiveChat().GetInputPeer()
		_ = b.editOrSendMessage(ctx, peer, cbq.MsgID,
			"⚠️ <b>Upload Expired</b>\n\nThis upload session has expired. Please resend the file.",
			nil,
		)
		return nil
	}
	b.pending.Delete(key)

	// Update status message
	_ = b.editOrSendMessage(ctx, pending.InputPeer, cbq.MsgID,
		fmt.Sprintf("⏳ <b>Stashing <code>%s</code>...</b>\n\n<i>Downloading via MTProto directly to server storage...</i>", htmlEscape(pending.Filename)),
		nil,
	)

	// Execute download and persistence in a background goroutine so dispatcher is never blocked
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("panic while processing file download",
					"panic", r,
					"stack", string(debug.Stack()),
				)
				_ = b.editOrSendMessage(ctx, pending.InputPeer, cbq.MsgID,
					"❌ <b>Internal Error</b>: An error occurred while processing the file.",
					nil,
				)
			}
		}()

		b.processAndStash(ctx, chatID, cbq.MsgID, expiryChoice, pending)
	}()

	return nil
}

// handleDeleteCallback processes inline deletion requests ("del:<fileID>").
// Deletes disk storage and database records directly in-process with zero API calls,
// and deletes the message from Telegram. If file is expired or missing, it informs the user and removes the message.
func (b *BotService) handleDeleteCallback(ctx *ext.Context, update *ext.Update) error {
	cbq := update.CallbackQuery
	if cbq == nil {
		return nil
	}

	data := string(cbq.Data)
	parts := strings.Split(data, ":")
	if len(parts) != 2 {
		_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
			Alert:   true,
			QueryID: cbq.QueryID,
			Message: "⚠️ Invalid callback data.",
		})
		return nil
	}
	fileID := parts[1]
	chatID := update.EffectiveChat().GetID()

	senderUser := update.EffectiveUser()
	var senderID int64
	if senderUser != nil {
		senderID = senderUser.ID
	}

	if b.limiter != nil && !b.limiter.Allow(senderID) {
		_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
			QueryID: cbq.QueryID,
			Message: "⏳ Please wait a moment before tapping again.",
			Alert:   false,
		})
		return nil
	}

	// Query file from SQLite database
	file, err := b.db.GetFile(ctx.Context, fileID)
	if err != nil || file == nil || time.Now().After(file.ExpiresAt) {
		// File does not exist or has already expired
		if file != nil {
			_ = b.storage.Delete(file.StoragePath)
			_ = b.db.SoftDeleteByID(ctx.Context, fileID)
		}

		_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
			Alert:   true,
			QueryID: cbq.QueryID,
			Message: "⚠️ File does not exist or has already expired.",
		})

		// Delete the Telegram message
		_ = ctx.DeleteMessages(chatID, []int{cbq.MsgID})
		return nil
	}

	// Authorization check: original uploader OR whitelisted user
	uploaderPrefix := fmt.Sprintf("telegram:%d", senderID)
	if file.UploaderIP != uploaderPrefix && !b.cfg.IsTelegramUserAllowed(senderID) {
		_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
			Alert:   true,
			QueryID: cbq.QueryID,
			Message: "⛔ You are not authorized to delete this file.",
		})
		return nil
	}

	// Direct in-process deletion: remove disk file and soft-delete in SQLite
	_ = b.storage.Delete(file.StoragePath)
	if err := b.db.SoftDeleteByID(ctx.Context, fileID); err != nil {
		b.logger.Error("failed to mark file deleted in database", "id", fileID, "error", err)
	}

	b.logger.Info("file deleted via telegram bot inline button",
		"id", fileID,
		"user_id", senderID,
	)

	_, _ = ctx.AnswerCallback(&tg.MessagesSetBotCallbackAnswerRequest{
		Alert:   true,
		QueryID: cbq.QueryID,
		Message: "🗑 File and download links deleted successfully.",
	})

	// Delete the Telegram message
	_ = ctx.DeleteMessages(chatID, []int{cbq.MsgID})
	return nil
}

// processAndStash streams the file directly to Warpstash disk storage and SQLite.
func (b *BotService) processAndStash(ctx *ext.Context, _ int64, promptMsgID int, expiryChoice string, pending *PendingUpload) {
	// Create scratch directory for MTProto transfer
	tmpDir := filepath.Join(b.cfg.StoragePath, ".bot_tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		b.logger.Error("failed to create bot tmp dir", "error", err)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, "❌ <b>Server Error</b>: Failed to initialize temporary storage.", nil)
		return
	}

	tmpFile, err := os.CreateTemp(tmpDir, "tg_download_*")
	if err != nil {
		b.logger.Error("failed to create temp file", "error", err)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, "❌ <b>Server Error</b>: Failed to create temporary file.", nil)
		return
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	// Download media using gotgproto with parallel chunk streaming
	_, err = ctx.DownloadMedia(
		pending.Media,
		ext.DownloadOutputPath(tmpPath),
		&ext.DownloadMediaOpts{
			Threads: 8,
		},
	)
	if err != nil {
		b.logger.Error("failed to download media from telegram", "error", err, "file", pending.Filename)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID,
			fmt.Sprintf("❌ <b>Download Failed</b>\n\nTelegram MTProto transfer failed: %s", htmlEscape(err.Error())),
			nil,
		)
		return
	}

	// Open downloaded file for streaming into sharded storage
	f, err := os.Open(tmpPath)
	if err != nil {
		b.logger.Error("failed to open downloaded temp file", "error", err)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, "❌ <b>Server Error</b>: Failed to read downloaded file.", nil)
		return
	}
	defer f.Close()

	// Generate 10-char NanoID
	id, err := util.GenerateID(10)
	if err != nil {
		b.logger.Error("failed to generate ID", "error", err)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, "❌ <b>Server Error</b>: ID generation failed.", nil)
		return
	}

	// Persist to Warpstash sharded disk storage engine
	storagePath, sizeBytes, sha256Hex, err := b.storage.SaveStream(ctx.Context, id, f, b.cfg.MaxFileSizeBytes())
	if err != nil {
		b.logger.Error("failed to persist file to storage", "error", err, "id", id)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID,
			fmt.Sprintf("❌ <b>Storage Failed</b>\n\nFailed to persist file: %s", htmlEscape(err.Error())),
			nil,
		)
		return
	}

	// Parse expiration duration and burn flag
	dur, isBurn, err := b.cfg.ParseExpiry(expiryChoice)
	if err != nil {
		_ = b.storage.Delete(storagePath)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID,
			fmt.Sprintf("❌ <b>Invalid Expiry</b>: %s", htmlEscape(err.Error())),
			nil,
		)
		return
	}

	// Generate 32-byte crypto deletion token
	deleteToken, err := util.GenerateDeleteToken()
	if err != nil {
		_ = b.storage.Delete(storagePath)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, "❌ <b>Server Error</b>: Delete token generation failed.", nil)
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(dur)
	sanitizedName := util.SanitizeFilename(pending.Filename)
	ext := filepath.Ext(sanitizedName)

	record := &database.FileRecord{
		ID:           id,
		OriginalName: sanitizedName,
		Extension:    ext,
		SizeBytes:    sizeBytes,
		MimeType:     pending.MimeType,
		SHA256Hash:   sha256Hex,
		StoragePath:  storagePath,
		DeleteToken:  deleteToken,
		UploaderIP:   fmt.Sprintf("telegram:%d", pending.UserID),
		IsBurnOnRead: isBurn,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
	}

	if err := b.db.InsertFile(ctx.Context, record); err != nil {
		_ = b.storage.Delete(storagePath)
		b.logger.Error("failed to record file in database", "error", err, "id", id)
		_ = b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, "❌ <b>Database Error</b>: Failed to save file metadata.", nil)
		return
	}

	b.logger.Info("file stashed successfully via telegram bot",
		"id", id,
		"name", sanitizedName,
		"size", sizeBytes,
		"user_id", pending.UserID,
		"burn", isBurn,
		"expires_in", dur.String(),
	)

	// Construct public URLs
	fileURL := fmt.Sprintf("%s/f/%s%s", b.cfg.BaseURL, id, ext)

	expiryDisplay := expiryChoice
	if isBurn {
		expiryDisplay = "🔥 Burn on read (single download)"
	}

	hasDownloadButton := isValidTelegramButtonURL(fileURL)
	successMsg := formatSuccessMessage(sanitizedName, sizeBytes, expiryDisplay, fileURL, hasDownloadButton)

	var rows []tg.KeyboardButtonRow
	if hasDownloadButton {
		rows = append(rows, tg.KeyboardButtonRow{
			Buttons: []tg.KeyboardButtonClass{
				&tg.KeyboardButtonURL{
					Text: "🔗 Open Download Link",
					URL:  fileURL,
				},
			},
		})
	}

	// Inline callback button to delete the file immediately
	rows = append(rows, tg.KeyboardButtonRow{
		Buttons: []tg.KeyboardButtonClass{
			&tg.KeyboardButtonCallback{
				Text: "🗑 Delete File",
				Data: fmt.Appendf(nil, "del:%s", id),
			},
		},
	})

	buttonMarkup := &tg.ReplyInlineMarkup{Rows: rows}

	if err := b.editOrSendMessage(ctx, pending.InputPeer, promptMsgID, successMsg, buttonMarkup); err != nil {
		b.logger.Error("failed to deliver success message to user", "error", err)
	}
}

// formatSuccessMessage formats the Telegram notification for stashed files.
// If an inline download button is available, the download link is omitted from text.
// Otherwise, it is included in text. Delete links are omitted since an inline button is provided.
func formatSuccessMessage(sanitizedName string, sizeBytes int64, expiryDisplay, fileURL string, hasDownloadButton bool) string {
	msg := fmt.Sprintf(
		"✅ <b>File Stashed Successfully!</b>\n\n"+
			"📁 <b>Name:</b> <code>%s</code>\n"+
			"📦 <b>Size:</b> <code>%s</code>\n"+
			"⏳ <b>Retention:</b> <code>%s</code>",
		htmlEscape(sanitizedName),
		humanize.Bytes(uint64(sizeBytes)),
		expiryDisplay,
	)
	if !hasDownloadButton {
		msg += fmt.Sprintf("\n\n🔗 <b>Download Link:</b>\n<code>%s</code>", fileURL)
	}
	return msg
}

// buildExpiryMarkup builds the inline keyboard rows based on allowed expiries.
func (b *BotService) buildExpiryMarkup(msgID int) *tg.ReplyInlineMarkup {
	var rows []tg.KeyboardButtonRow
	var currentRow []tg.KeyboardButtonClass

	for _, exp := range b.cfg.AllowedExpiries {
		label := exp
		if strings.EqualFold(exp, "burn") {
			label = "🔥 Burn"
		}
		cbData := fmt.Sprintf("exp:%s:%d", exp, msgID)
		btn := &tg.KeyboardButtonCallback{
			Text: label,
			Data: []byte(cbData),
		}
		currentRow = append(currentRow, btn)
		if len(currentRow) == 3 {
			rows = append(rows, tg.KeyboardButtonRow{Buttons: currentRow})
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, tg.KeyboardButtonRow{Buttons: currentRow})
	}
	return &tg.ReplyInlineMarkup{Rows: rows}
}

// isNilMarkup guards against Go's typed nil interface trap (e.g. (*tg.ReplyInlineMarkup)(nil)).
func isNilMarkup(markup tg.ReplyMarkupClass) bool {
	if markup == nil {
		return true
	}
	v := reflect.ValueOf(markup)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// isValidTelegramButtonURL checks whether a URL is acceptable by Telegram MTProto for inline buttons.
// Telegram strictly rejects buttons pointing to localhost, loopback addresses, or private IPs with BUTTON_URL_INVALID.
func isValidTelegramButtonURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" || hostname == "localhost" || strings.HasSuffix(hostname, ".local") {
		return false
	}
	ip := net.ParseIP(hostname)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
			return false
		}
		return true
	}
	// Telegram generally requires a dot in domain name (e.g. example.com) or public IP
	return strings.Contains(hostname, ".")
}

// editOrSendMessage edits the target message in-place, or falls back to sending a new message if editing is rejected.
func (b *BotService) editOrSendMessage(ctx *ext.Context, peer tg.InputPeerClass, msgID int, htmlContent string, markup tg.ReplyMarkupClass) error {
	if isNilMarkup(markup) {
		markup = nil
	}

	var tb entity.Builder
	if err := styling.Perform(&tb, styling.Custom(func(eb *entity.Builder) error {
		return html.HTML(strings.NewReader(htmlContent), eb, html.Options{})
	})); err != nil {
		tb.Reset()
		_, _ = tb.WriteString(htmlContent)
	}
	text, entities := tb.Complete()

	// 1. Attempt to edit the existing prompt message
	editReq := &tg.MessagesEditMessageRequest{
		Peer:        peer,
		ID:          msgID,
		Message:     text,
		Entities:    entities,
		NoWebpage:   true,
		ReplyMarkup: markup,
	}

	_, editErr := ctx.Raw.MessagesEditMessage(ctx, editReq)
	if editErr == nil {
		return nil
	}

	// If Telegram or serialization rejected markup, retry edit without markup
	if markup != nil {
		editReq.ReplyMarkup = nil
		_, retryErr := ctx.Raw.MessagesEditMessage(ctx, editReq)
		if retryErr == nil {
			return nil
		}
	}

	b.logger.Warn("edit message rejected by Telegram, falling back to new message",
		"msg_id", msgID,
		"error", editErr,
	)

	// 2. Fallback: Send fresh message to ensure the user receives the link
	var randID int64
	_ = binary.Read(crand.Reader, binary.LittleEndian, &randID)

	sendReq := &tg.MessagesSendMessageRequest{
		Peer:        peer,
		Message:     text,
		Entities:    entities,
		NoWebpage:   true,
		ReplyMarkup: markup,
		RandomID:    randID,
	}

	_, sendErr := ctx.Raw.MessagesSendMessage(ctx, sendReq)
	if sendErr == nil {
		return nil
	}

	// If sending with markup failed, retry send without markup
	if markup != nil {
		sendReq.ReplyMarkup = nil
		_ = binary.Read(crand.Reader, binary.LittleEndian, &sendReq.RandomID)
		_, retrySendErr := ctx.Raw.MessagesSendMessage(ctx, sendReq)
		if retrySendErr == nil {
			return nil
		}
		sendErr = retrySendErr
	}

	b.logger.Error("failed to send fallback message", "error", sendErr)
	return sendErr
}

// extractMediaInfo extracts filename, size, and mime type from various Telegram media formats.
func (b *BotService) extractMediaInfo(media tg.MessageMediaClass) (filename string, size int64, mimeType string, err error) {
	switch m := media.(type) {
	case *tg.MessageMediaDocument:
		doc, ok := m.Document.AsNotEmpty()
		if !ok {
			return "", 0, "", errors.New("empty document")
		}
		size = doc.Size
		mimeType = doc.MimeType

		for _, attr := range doc.Attributes {
			if fnAttr, ok := attr.(*tg.DocumentAttributeFilename); ok {
				filename = fnAttr.FileName
				break
			}
		}

		if filename == "" {
			for _, attr := range doc.Attributes {
				if _, ok := attr.(*tg.DocumentAttributeVideo); ok {
					filename = fmt.Sprintf("video_%d.mp4", doc.ID)
					break
				}
				if a, ok := attr.(*tg.DocumentAttributeAudio); ok {
					if a.Voice {
						filename = fmt.Sprintf("voice_%d.ogg", doc.ID)
					} else {
						filename = fmt.Sprintf("audio_%d.mp3", doc.ID)
					}
					break
				}
			}
		}

		if filename == "" {
			ext := ""
			if mimeType != "" {
				exts, _ := mime.ExtensionsByType(mimeType)
				if len(exts) > 0 {
					ext = exts[0]
				}
			}
			if ext == "" {
				ext = ".bin"
			}
			filename = fmt.Sprintf("file_%d%s", doc.ID, ext)
		}

		return util.SanitizeFilename(filename), size, mimeType, nil

	case *tg.MessageMediaPhoto:
		photo, ok := m.Photo.AsNotEmpty()
		if !ok {
			return "", 0, "", errors.New("empty photo")
		}
		filename = fmt.Sprintf("photo_%d.jpg", photo.ID)
		mimeType = "image/jpeg"
		if len(photo.Sizes) > 0 {
			lastSize := photo.Sizes[len(photo.Sizes)-1]
			switch s := lastSize.(type) {
			case *tg.PhotoSize:
				size = int64(s.Size)
			case *tg.PhotoSizeProgressive:
				if len(s.Sizes) > 0 {
					size = int64(s.Sizes[len(s.Sizes)-1])
				}
			}
		}
		return filename, size, mimeType, nil

	default:
		return "", 0, "", errors.New("unsupported media type")
	}
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
