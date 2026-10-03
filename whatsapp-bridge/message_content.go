package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func joinMessageText(parts ...string) string {
	var text []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			text = append(text, trimmed)
		}
	}
	return strings.Join(text, "\n")
}

// Business templates can nest (template -> HSM -> hydrated template -> ...).
// Bound traversal so a malformed or cyclic payload cannot exhaust the stack.
const maxBusinessMessageDepth = 8

// unwrapMessage removes the envelopes WhatsApp puts around the actual content:
// disappearing messages, view once, messages sent from another of our devices,
// documents with captions, mentions and so on. whatsmeow already unwraps live
// messages, but history sync hands over the raw envelope, which used to make
// every message in a chat with disappearing messages look empty.
func unwrapMessage(msg *waProto.Message) *waProto.Message {
	for depth := 0; msg != nil && depth <= maxBusinessMessageDepth; depth++ {
		var inner *waProto.Message
		switch {
		case msg.GetDeviceSentMessage().GetMessage() != nil:
			inner = msg.GetDeviceSentMessage().GetMessage()
		case msg.GetEphemeralMessage().GetMessage() != nil:
			inner = msg.GetEphemeralMessage().GetMessage()
		case msg.GetViewOnceMessage().GetMessage() != nil:
			inner = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2().GetMessage() != nil:
			inner = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension().GetMessage() != nil:
			inner = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetDocumentWithCaptionMessage().GetMessage() != nil:
			inner = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetLottieStickerMessage().GetMessage() != nil:
			inner = msg.GetLottieStickerMessage().GetMessage()
		case msg.GetGroupMentionedMessage().GetMessage() != nil:
			inner = msg.GetGroupMentionedMessage().GetMessage()
		case msg.GetBotInvokeMessage().GetMessage() != nil:
			inner = msg.GetBotInvokeMessage().GetMessage()
		case msg.GetSpoilerMessage().GetMessage() != nil:
			inner = msg.GetSpoilerMessage().GetMessage()
		case msg.GetAssociatedChildMessage().GetMessage() != nil:
			inner = msg.GetAssociatedChildMessage().GetMessage()
		case msg.GetPollCreationMessageV4().GetMessage() != nil:
			inner = msg.GetPollCreationMessageV4().GetMessage()
		case msg.GetEditedMessage().GetMessage() != nil:
			inner = msg.GetEditedMessage().GetMessage()
		}
		if inner == nil {
			return msg
		}
		msg = inner
	}
	return msg
}

// formatMessageTime renders a WhatsApp timestamp in the bridge's local time
// zone with an explicit offset, so a 09:00 appointment in São Paulo is not
// read as noon UTC.
func formatMessageTime(unix int64) string {
	return time.Unix(unix, 0).In(time.Local).Format("2006-01-02 15:04 -07:00")
}

func formatButton(label, target string) string {
	label = strings.TrimSpace(label)
	target = strings.TrimSpace(target)
	switch {
	case label == "" && target == "":
		return ""
	case target == "":
		return fmt.Sprintf("[button: %s]", label)
	case label == "":
		return fmt.Sprintf("[button: %s]", target)
	default:
		return fmt.Sprintf("[button: %s → %s]", label, target)
	}
}

func extractHydratedButtonsText(buttons []*waProto.HydratedTemplateButton) []string {
	var parts []string
	for _, button := range buttons {
		switch {
		case button.GetUrlButton() != nil:
			parts = append(parts, formatButton(button.GetUrlButton().GetDisplayText(), button.GetUrlButton().GetURL()))
		case button.GetCallButton() != nil:
			parts = append(parts, formatButton(button.GetCallButton().GetDisplayText(), button.GetCallButton().GetPhoneNumber()))
		case button.GetQuickReplyButton() != nil:
			parts = append(parts, formatButton(button.GetQuickReplyButton().GetDisplayText(), ""))
		}
	}
	return parts
}

func extractHydratedTemplateText(template *waProto.TemplateMessage_HydratedFourRowTemplate) string {
	if template == nil {
		return ""
	}
	parts := []string{
		template.GetHydratedTitleText(),
		template.GetHydratedContentText(),
		template.GetHydratedFooterText(),
	}
	// Buttons alone are not content: a template whose text is empty is still
	// treated as empty so the caller can fall back to other formats.
	if joinMessageText(parts...) == "" {
		return ""
	}
	return joinMessageText(append(parts, extractHydratedButtonsText(template.GetHydratedButtons())...)...)
}

// nativeFlowButtonText reads the label and target (link, PIX copy code,
// phone number) of an interactive "native flow" button. Its parameters are a
// JSON string whose shape depends on the button kind.
func nativeFlowButtonText(button *waProto.InteractiveMessage_NativeFlowMessage_NativeFlowButton) string {
	var params map[string]any
	if err := json.Unmarshal([]byte(button.GetButtonParamsJSON()), &params); err != nil {
		return ""
	}
	label, _ := params["display_text"].(string)
	if label == "" {
		return ""
	}
	for _, key := range []string{"url", "copy_code", "phone_number"} {
		if target, ok := params[key].(string); ok && target != "" {
			return formatButton(label, target)
		}
	}
	return formatButton(label, "")
}

func extractInteractiveMessageText(message *waProto.InteractiveMessage, depth int) string {
	if message == nil || depth > maxBusinessMessageDepth {
		return ""
	}
	parts := []string{
		message.GetHeader().GetTitle(),
		message.GetHeader().GetSubtitle(),
		message.GetBody().GetText(),
		message.GetFooter().GetText(),
	}
	for _, card := range message.GetCarouselMessage().GetCards() {
		parts = append(parts, extractInteractiveMessageText(card, depth+1))
	}
	if joinMessageText(parts...) == "" {
		return ""
	}
	for _, button := range message.GetNativeFlowMessage().GetButtons() {
		parts = append(parts, nativeFlowButtonText(button))
	}
	return joinMessageText(parts...)
}

func extractTemplateMessageText(template *waProto.TemplateMessage, depth int) string {
	if template == nil || depth > maxBusinessMessageDepth {
		return ""
	}
	if text := extractHydratedTemplateText(template.GetHydratedTemplate()); text != "" {
		return text
	}
	if text := extractHydratedTemplateText(template.GetHydratedFourRowTemplate()); text != "" {
		return text
	}
	if text := extractInteractiveMessageText(template.GetInteractiveMessageTemplate(), depth+1); text != "" {
		return text
	}
	if fourRow := template.GetFourRowTemplate(); fourRow != nil {
		rows := []*waProto.HighlyStructuredMessage{
			fourRow.GetHighlyStructuredMessage(),
			fourRow.GetContent(),
			fourRow.GetFooter(),
		}
		var parts []string
		name := ""
		hydrated := false
		for _, row := range rows {
			text, rowHydrated := highlyStructuredMessageBody(row, depth+1)
			parts = append(parts, text)
			hydrated = hydrated || rowHydrated
			if name == "" {
				name = strings.TrimSpace(row.GetElementName())
			}
		}
		// Label the template once, not once per row.
		if !hydrated {
			parts = append([]string{businessTemplateLabel(name)}, parts...)
		}
		return joinMessageText(parts...)
	}
	return ""
}

func formatLocalizableParam(param *waProto.HighlyStructuredMessage_HSMLocalizableParameter) string {
	if param == nil {
		return ""
	}
	if value := strings.TrimSpace(param.GetDefault()); value != "" {
		return value
	}
	if currency := param.GetCurrency(); currency != nil {
		return strings.TrimSpace(fmt.Sprintf("%s %.2f", currency.GetCurrencyCode(), float64(currency.GetAmount1000())/1000))
	}
	if dateTime := param.GetDateTime(); dateTime != nil {
		if epoch := dateTime.GetUnixEpoch(); epoch != nil {
			return formatMessageTime(epoch.GetTimestamp())
		}
		if c := dateTime.GetComponent(); c != nil {
			return fmt.Sprintf("%04d-%02d-%02d %02d:%02d", c.GetYear(), c.GetMonth(), c.GetDayOfMonth(), c.GetHour(), c.GetMinute())
		}
	}
	return ""
}

func businessTemplateLabel(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return fmt.Sprintf("[business template: %s]", name)
	}
	return "[business template]"
}

// highlyStructuredMessageBody returns the text of an HSM without the template
// label, and whether it came from a hydrated (fully rendered) template.
func highlyStructuredMessageBody(message *waProto.HighlyStructuredMessage, depth int) (string, bool) {
	if message == nil || depth > maxBusinessMessageDepth {
		return "", false
	}
	if text := extractTemplateMessageText(message.GetHydratedHsm(), depth+1); text != "" {
		return text, true
	}
	parts := append([]string{}, message.GetParams()...)
	for _, param := range message.GetLocalizableParams() {
		parts = append(parts, formatLocalizableParam(param))
	}
	return joinMessageText(parts...), false
}

func extractHighlyStructuredMessageText(message *waProto.HighlyStructuredMessage, depth int) string {
	if message == nil || depth > maxBusinessMessageDepth {
		return ""
	}
	text, hydrated := highlyStructuredMessageBody(message, depth)
	if hydrated {
		return text
	}
	return joinMessageText(businessTemplateLabel(message.GetElementName()), text)
}

func extractButtonsMessageText(message *waProto.ButtonsMessage) string {
	if message == nil {
		return ""
	}
	parts := []string{message.GetText(), message.GetContentText(), message.GetFooterText()}
	if joinMessageText(parts...) == "" {
		return ""
	}
	for _, button := range message.GetButtons() {
		parts = append(parts, formatButton(button.GetButtonText().GetDisplayText(), ""))
	}
	return joinMessageText(parts...)
}

func extractListMessageText(message *waProto.ListMessage) string {
	if message == nil {
		return ""
	}
	parts := []string{message.GetTitle(), message.GetDescription()}
	for _, section := range message.GetSections() {
		parts = append(parts, section.GetTitle())
		for _, row := range section.GetRows() {
			if row.GetDescription() != "" {
				parts = append(parts, fmt.Sprintf("- %s: %s", row.GetTitle(), row.GetDescription()))
			} else if row.GetTitle() != "" {
				parts = append(parts, "- "+row.GetTitle())
			}
		}
	}
	parts = append(parts, message.GetFooterText())
	return joinMessageText(parts...)
}

// extractReplyText covers what the user (or the other side) picked in a
// business message: a button, a list row or an interactive flow.
func extractReplyText(msg *waProto.Message) string {
	if reply := msg.GetButtonsResponseMessage(); reply != nil {
		if text := reply.GetSelectedDisplayText(); text != "" {
			return text
		}
		if id := reply.GetSelectedButtonID(); id != "" {
			return fmt.Sprintf("[button reply: %s]", id)
		}
	}
	if reply := msg.GetListResponseMessage(); reply != nil {
		if text := joinMessageText(reply.GetTitle(), reply.GetDescription()); text != "" {
			return text
		}
		if id := reply.GetSingleSelectReply().GetSelectedRowID(); id != "" {
			return fmt.Sprintf("[list reply: %s]", id)
		}
	}
	if reply := msg.GetTemplateButtonReplyMessage(); reply != nil {
		if text := reply.GetSelectedDisplayText(); text != "" {
			return text
		}
		if id := reply.GetSelectedID(); id != "" {
			return fmt.Sprintf("[button reply: %s]", id)
		}
	}
	if reply := msg.GetInteractiveResponseMessage(); reply != nil {
		if text := reply.GetBody().GetText(); text != "" {
			return text
		}
		if name := reply.GetNativeFlowResponseMessage().GetName(); name != "" {
			return fmt.Sprintf("[interactive reply: %s]", name)
		}
	}
	return ""
}

func mapsLink(latitude, longitude float64) string {
	return fmt.Sprintf("https://maps.google.com/?q=%.6f,%.6f", latitude, longitude)
}

func extractLocationText(msg *waProto.Message) string {
	if loc := msg.GetLocationMessage(); loc != nil {
		label := "[location]"
		if name := strings.TrimSpace(loc.GetName()); name != "" {
			label = fmt.Sprintf("[location: %s]", name)
		}
		return joinMessageText(
			label,
			loc.GetAddress(),
			loc.GetComment(),
			mapsLink(loc.GetDegreesLatitude(), loc.GetDegreesLongitude()),
			loc.GetURL(),
		)
	}
	if loc := msg.GetLiveLocationMessage(); loc != nil {
		return joinMessageText(
			"[live location]",
			loc.GetCaption(),
			mapsLink(loc.GetDegreesLatitude(), loc.GetDegreesLongitude()),
		)
	}
	return ""
}

var vcardPhonePattern = regexp.MustCompile(`(?mi)^(?:item\d+\.)?TEL[^:\r\n]*:([^\r\n]+)\r?$`)

func vcardPhones(vcard string) []string {
	var phones []string
	for _, match := range vcardPhonePattern.FindAllStringSubmatch(vcard, -1) {
		if phone := strings.TrimSpace(match[1]); phone != "" {
			phones = append(phones, phone)
		}
	}
	return phones
}

func formatContact(contact *waProto.ContactMessage) string {
	name := strings.TrimSpace(contact.GetDisplayName())
	phones := strings.Join(vcardPhones(contact.GetVcard()), ", ")
	switch {
	case name != "" && phones != "":
		return fmt.Sprintf("%s: %s", name, phones)
	case name != "":
		return name
	default:
		return phones
	}
}

func extractContactText(msg *waProto.Message) string {
	if contact := msg.GetContactMessage(); contact != nil {
		return joinMessageText("[contact]", formatContact(contact))
	}
	if contacts := msg.GetContactsArrayMessage(); contacts != nil {
		label := "[contacts]"
		if name := strings.TrimSpace(contacts.GetDisplayName()); name != "" {
			label = fmt.Sprintf("[contacts: %s]", name)
		}
		parts := []string{label}
		for _, contact := range contacts.GetContacts() {
			parts = append(parts, formatContact(contact))
		}
		return joinMessageText(parts...)
	}
	return ""
}

func extractPollText(msg *waProto.Message) string {
	for _, poll := range []*waProto.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
		msg.GetPollCreationMessageV5(),
		msg.GetPollCreationMessageV6(),
	} {
		if poll == nil {
			continue
		}
		parts := []string{fmt.Sprintf("[poll: %s]", strings.TrimSpace(poll.GetName()))}
		for _, option := range poll.GetOptions() {
			if name := strings.TrimSpace(option.GetOptionName()); name != "" {
				parts = append(parts, "- "+name)
			}
		}
		return joinMessageText(parts...)
	}
	return ""
}

func extractEventText(msg *waProto.Message) string {
	event := msg.GetEventMessage()
	if event == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("[event: %s]", strings.TrimSpace(event.GetName()))}
	if event.GetIsCanceled() {
		parts = append(parts, "[canceled]")
	}
	parts = append(parts, event.GetDescription())
	if start := event.GetStartTime(); start != 0 {
		when := formatMessageTime(start)
		if end := event.GetEndTime(); end != 0 {
			when += " to " + formatMessageTime(end)
		}
		parts = append(parts, when)
	}
	if location := event.GetLocation(); location != nil {
		parts = append(parts, joinMessageText(location.GetName(), location.GetAddress()))
	}
	parts = append(parts, event.GetJoinLink())
	return joinMessageText(parts...)
}

func extractGroupInviteText(msg *waProto.Message) string {
	invite := msg.GetGroupInviteMessage()
	if invite == nil {
		return ""
	}
	return joinMessageText(fmt.Sprintf("[group invite: %s]", strings.TrimSpace(invite.GetGroupName())), invite.GetCaption())
}

func extractCaptionText(msg *waProto.Message) string {
	switch {
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetCaption()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetCaption()
	case msg.GetPtvMessage() != nil:
		return msg.GetPtvMessage().GetCaption()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetCaption()
	}
	return ""
}

// Extract text content from a message
func extractTextContent(msg *waProto.Message) string {
	msg = unwrapMessage(msg)
	if msg == nil {
		return ""
	}

	if text := msg.GetConversation(); text != "" {
		return text
	}
	if text := msg.GetExtendedTextMessage().GetText(); text != "" {
		return text
	}

	extractors := []func(*waProto.Message) string{
		extractCaptionText,
		func(m *waProto.Message) string { return extractTemplateMessageText(m.GetTemplateMessage(), 0) },
		func(m *waProto.Message) string {
			return extractHighlyStructuredMessageText(m.GetHighlyStructuredMessage(), 0)
		},
		func(m *waProto.Message) string { return extractInteractiveMessageText(m.GetInteractiveMessage(), 0) },
		func(m *waProto.Message) string { return extractButtonsMessageText(m.GetButtonsMessage()) },
		func(m *waProto.Message) string { return extractListMessageText(m.GetListMessage()) },
		extractReplyText,
		extractLocationText,
		extractContactText,
		extractPollText,
		extractEventText,
		extractGroupInviteText,
	}
	for _, extract := range extractors {
		if text := extract(msg); text != "" {
			return text
		}
	}
	return ""
}

// mediaHeader is satisfied by the business message parts that can carry an
// attachment in their header: hydrated templates, interactive headers and
// buttons messages.
type mediaHeader interface {
	GetImageMessage() *waProto.ImageMessage
	GetVideoMessage() *waProto.VideoMessage
	GetDocumentMessage() *waProto.DocumentMessage
}

// businessHeaderMedia returns the attachment of a business message (a PDF
// invoice in a template header, for instance) as a plain media message, so it
// is stored and downloadable like any other media.
func businessHeaderMedia(msg *waProto.Message) *waProto.Message {
	template := msg.GetTemplateMessage()
	headers := []mediaHeader{
		template.GetHydratedTemplate(),
		template.GetHydratedFourRowTemplate(),
		template.GetInteractiveMessageTemplate().GetHeader(),
		msg.GetHighlyStructuredMessage().GetHydratedHsm().GetHydratedTemplate(),
		msg.GetInteractiveMessage().GetHeader(),
		msg.GetButtonsMessage(),
	}
	for _, header := range headers {
		switch {
		case header.GetImageMessage() != nil:
			return &waProto.Message{ImageMessage: header.GetImageMessage()}
		case header.GetVideoMessage() != nil:
			return &waProto.Message{VideoMessage: header.GetVideoMessage()}
		case header.GetDocumentMessage() != nil:
			return &waProto.Message{DocumentMessage: header.GetDocumentMessage()}
		}
	}
	return nil
}

// populatedMessageFields lists the proto fields set on a message, sorted for
// stable logs. Unknown field numbers are reported too, since fields newer than
// the pinned descriptors have no name.
func populatedMessageFields(message *waProto.Message) []string {
	if message == nil {
		return nil
	}
	var fields []string
	reflected := message.ProtoReflect()
	reflected.Range(func(field protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		fields = append(fields, string(field.Name()))
		return true
	})
	if unknown := reflected.GetUnknown(); len(unknown) > 0 {
		fields = append(fields, fmt.Sprintf("unknown(%d bytes)", len(unknown)))
	}
	sort.Strings(fields)
	return fields
}

// expectedNonContentFields are envelopes that never carry content to store:
// encryption key exchange, reactions, poll votes and protocol operations such
// as edits and deletions. They make up most dropped messages, so they are only
// logged at debug level.
var expectedNonContentFields = map[string]bool{
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
	"messageContextInfo":                         true,
	"protocolMessage":                            true,
	"reactionMessage":                            true,
	"encReactionMessage":                         true,
	"pollUpdateMessage":                          true,
	"keepInChatMessage":                          true,
	"pinInChatMessage":                           true,
	"encEventResponseMessage":                    true,
	"encCommentMessage":                          true,
	"stickerSyncRmrMessage":                      true,
}

func isExpectedNonContent(fields []string) bool {
	for _, field := range fields {
		if !expectedNonContentFields[field] {
			return false
		}
	}
	return true
}

// logDroppedMessage records why a message was not stored, so new WhatsApp
// envelopes are discoverable instead of silently disappearing.
func logDroppedMessage(logger waLog.Logger, id, chatJID string, message *waProto.Message) {
	fields := populatedMessageFields(message)
	log := logger.Warnf
	if isExpectedNonContent(fields) {
		log = logger.Debugf
	}
	log(
		"Dropping message %s in %s: no text or media extracted; populated fields: %s",
		id,
		chatJID,
		strings.Join(fields, ", "),
	)
}

// droppedMessageTally counts unsupported envelopes during history sync, so
// pairing a device with a large archive logs one summary line instead of one
// warning per message.
type droppedMessageTally map[string]int

func (t droppedMessageTally) add(logger waLog.Logger, id, chatJID string, message *waProto.Message) {
	fields := populatedMessageFields(message)
	if isExpectedNonContent(fields) {
		logger.Debugf("Dropping history message %s in %s: populated fields: %s", id, chatJID, strings.Join(fields, ", "))
		return
	}
	t[strings.Join(fields, ", ")]++
}

func (t droppedMessageTally) summary() string {
	kinds := make([]string, 0, len(t))
	for kind := range t {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("[%s]=%d", kind, t[kind]))
	}
	return strings.Join(parts, "; ")
}
