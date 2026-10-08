// Package service capabilities encodes the channel support matrix that the
// MCP must respect before a write. It mirrors the Chatwoot channel reference:
// attachments and templates are not universal, so an unsupported channel is
// refused with a specific error instead of guessing.
package service

import (
	"fmt"
	"strings"
)

const (
	channelWebWidget = "Channel::WebWidget"
	channelAPI       = "Channel::Api"
	channelEmail     = "Channel::Email"
	channelTelegram  = "Channel::Telegram"
	channelWhatsapp  = "Channel::Whatsapp"
	channelTwilioSms = "Channel::TwilioSms"
	channelSms       = "Channel::Sms"
	channelLine      = "Channel::Line"
	channelFacebook  = "Channel::FacebookPage"
	channelInstagram = "Channel::Instagram"
	channelTiktok    = "Channel::Tiktok"
	channelTwitter   = "Channel::TwitterProfile"
)

// MaxAttachmentBytes is the default Chatwoot maximum upload size. A file above
// this cap is rejected before it is read into memory.
const MaxAttachmentBytes = 40 << 20

const (
	categoryImage    = "image"
	categoryAudio    = "audio"
	categoryVideo    = "video"
	categoryDocument = "document"
)

// isWhatsApp reports whether a channel is a WhatsApp channel, including the
// Twilio WhatsApp medium.
func isWhatsApp(channelType, medium string) bool {
	if channelType == channelWhatsapp {
		return true
	}
	return channelType == channelTwilioSms && medium == "whatsapp"
}

// mimeCategory groups a MIME type into the coarse category used by the channel
// matrix.
func mimeCategory(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return categoryImage
	case strings.HasPrefix(mimeType, "audio/"):
		return categoryAudio
	case strings.HasPrefix(mimeType, "video/"):
		return categoryVideo
	default:
		return categoryDocument
	}
}

// attachmentLimit returns the per-category byte limit for a channel and
// whether that channel accepts the category at all.
func attachmentLimit(channelType, medium, category string) (limit int64, supported bool) {
	switch channelType {
	case channelWebWidget, channelAPI, channelEmail, channelTelegram, channelTwitter:
		return MaxAttachmentBytes, true
	case channelFacebook:
		switch category {
		case categoryImage:
			return 8 << 20, true
		default:
			return 25 << 20, true
		}
	case channelInstagram:
		switch category {
		case categoryImage:
			return 16 << 20, true
		case categoryAudio, categoryVideo:
			return 25 << 20, true
		default:
			return 0, false
		}
	case channelWhatsapp:
		switch category {
		case categoryImage:
			return 5 << 20, true
		case categoryAudio, categoryVideo:
			return 16 << 20, true
		default:
			return MaxAttachmentBytes, true
		}
	case channelTwilioSms:
		if medium == "whatsapp" {
			return 5 << 20, true
		}
		return 5 << 20, true
	case channelLine:
		switch category {
		case categoryImage:
			return 10 << 20, true
		case categoryVideo:
			return MaxAttachmentBytes, true
		default:
			return 0, false
		}
	case channelTiktok:
		if category == categoryImage {
			return 3 << 20, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// attachmentAllowed reports whether an attachment of the given type and size
// may be uploaded to the channel, with a specific reason when it may not.
func attachmentAllowed(channelType, medium, mimeType string, size int64) (bool, string) {
	category := mimeCategory(mimeType)
	limit, supported := attachmentLimit(channelType, medium, category)
	if !supported {
		return false, fmt.Sprintf("the %s channel does not support %s attachments", channelLabel(channelType), category)
	}
	if limit > MaxAttachmentBytes {
		limit = MaxAttachmentBytes
	}
	if size > limit {
		return false, fmt.Sprintf("the %s channel allows at most %d bytes for %s attachments", channelLabel(channelType), limit, category)
	}
	return true, ""
}

func channelLabel(channelType string) string {
	label := strings.TrimPrefix(channelType, "Channel::")
	if label == "" {
		return "selected"
	}
	return label
}

// conversationInitiationAllowed reports whether a new outbound conversation can
// be started on the channel. Chatwoot allows initiation only on Website, API,
// Email and SMS/Phone channels; messaging channels require the contact to
// write first.
func conversationInitiationAllowed(channelType, medium string) bool {
	switch channelType {
	case channelWebWidget, channelAPI, channelEmail, channelSms:
		return true
	case channelTwilioSms:
		return medium != "whatsapp"
	default:
		return false
	}
}
