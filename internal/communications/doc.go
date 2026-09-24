// Package communications integrates Meta WhatsApp messaging and Twilio calling.
//
// Providers are isolated behind interfaces (WhatsAppProvider, TwilioProvider).
// WhatsApp never routes through Twilio. Both write into the unified CRM timeline.
package communications

const (
	ProviderMetaWhatsApp = "meta_whatsapp"
	ProviderTwilioVoice  = "twilio_voice"

	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"
)
