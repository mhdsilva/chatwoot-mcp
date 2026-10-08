package service

import "testing"

func TestIsWhatsApp(t *testing.T) {
	tests := []struct {
		channelType string
		medium      string
		want        bool
	}{
		{"Channel::Whatsapp", "", true},
		{"Channel::TwilioSms", "whatsapp", true},
		{"Channel::TwilioSms", "sms", false},
		{"Channel::WebWidget", "", false},
	}
	for _, test := range tests {
		if got := isWhatsApp(test.channelType, test.medium); got != test.want {
			t.Errorf("isWhatsApp(%q, %q) = %t, want %t", test.channelType, test.medium, got, test.want)
		}
	}
}

func TestAttachmentAllowedMatrix(t *testing.T) {
	tests := []struct {
		name        string
		channelType string
		medium      string
		mimeType    string
		size        int64
		wantOK      bool
	}{
		{"web widget image", channelWebWidget, "", "image/png", 10 << 20, true},
		{"api document", channelAPI, "", "application/pdf", 30 << 20, true},
		{"whatsapp image within limit", channelWhatsapp, "", "image/png", 4 << 20, true},
		{"whatsapp image over limit", channelWhatsapp, "", "image/png", 6 << 20, false},
		{"whatsapp document", channelWhatsapp, "", "application/pdf", 20 << 20, true},
		{"instagram document refused", channelInstagram, "", "application/pdf", 1 << 20, false},
		{"instagram image", channelInstagram, "", "image/png", 10 << 20, true},
		{"line audio refused", channelLine, "", "audio/ogg", 1 << 20, false},
		{"line image", channelLine, "", "image/png", 9 << 20, true},
		{"tiktok image only", channelTiktok, "", "image/jpeg", 1 << 20, true},
		{"tiktok video refused", channelTiktok, "", "video/mp4", 1 << 20, false},
		{"sms refused", channelSms, "", "image/png", 1 << 20, false},
		{"twilio whatsapp audio", channelTwilioSms, "whatsapp", "audio/ogg", 4 << 20, true},
		{"unknown channel refused", "Channel::Mystery", "", "image/png", 1 << 20, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok, reason := attachmentAllowed(test.channelType, test.medium, test.mimeType, test.size)
			if ok != test.wantOK {
				t.Fatalf("attachmentAllowed = %t (%s), want %t", ok, reason, test.wantOK)
			}
			if !ok && reason == "" {
				t.Fatal("refusal must carry a reason")
			}
		})
	}
}

func TestMimeCategory(t *testing.T) {
	cases := map[string]string{
		"image/png":       categoryImage,
		"audio/ogg":       categoryAudio,
		"video/mp4":       categoryVideo,
		"application/pdf": categoryDocument,
		"text/plain":      categoryDocument,
	}
	for mimeType, want := range cases {
		if got := mimeCategory(mimeType); got != want {
			t.Errorf("mimeCategory(%q) = %q, want %q", mimeType, got, want)
		}
	}
}
