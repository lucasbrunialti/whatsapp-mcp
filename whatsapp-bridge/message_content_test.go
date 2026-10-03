package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func TestExtractTextContentHydratedBusinessTemplate(t *testing.T) {
	msg := &waProto.Message{
		TemplateMessage: &waProto.TemplateMessage{
			HydratedTemplate: &waProto.TemplateMessage_HydratedFourRowTemplate{
				Title: &waProto.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{
					HydratedTitleText: "Appointment confirmed",
				},
				HydratedContentText: proto.String("Haircut on 2030-01-15 at 09:00"),
				HydratedFooterText:  proto.String("Please arrive on time"),
			},
		},
	}

	want := "Appointment confirmed\nHaircut on 2030-01-15 at 09:00\nPlease arrive on time"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentHydratedBusinessTemplateFormat(t *testing.T) {
	hydrated := &waProto.TemplateMessage_HydratedFourRowTemplate{
		HydratedContentText: proto.String("Your visit is confirmed for tomorrow at 09:00"),
	}
	msg := &waProto.Message{
		TemplateMessage: &waProto.TemplateMessage{
			Format: &waProto.TemplateMessage_HydratedFourRowTemplate_{
				HydratedFourRowTemplate: hydrated,
			},
		},
	}

	want := "Your visit is confirmed for tomorrow at 09:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentInteractiveBusinessMessage(t *testing.T) {
	msg := &waProto.Message{
		InteractiveMessage: &waProto.InteractiveMessage{
			Header: &waProto.InteractiveMessage_Header{
				Title:    proto.String("Confirmation"),
				Subtitle: proto.String("Example Salon"),
			},
			Body:   &waProto.InteractiveMessage_Body{Text: proto.String("Tuesday, 2030-01-15, 09:00")},
			Footer: &waProto.InteractiveMessage_Footer{Text: proto.String("Please arrive on time")},
		},
	}

	want := "Confirmation\nExample Salon\nTuesday, 2030-01-15, 09:00\nPlease arrive on time"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentInteractiveTemplateWrapper(t *testing.T) {
	interactive := &waProto.InteractiveMessage{
		Body: &waProto.InteractiveMessage_Body{Text: proto.String("Appointment confirmed at 09:00")},
	}
	msg := &waProto.Message{
		TemplateMessage: &waProto.TemplateMessage{
			Format: &waProto.TemplateMessage_InteractiveMessageTemplate{
				InteractiveMessageTemplate: interactive,
			},
		},
	}

	want := "Appointment confirmed at 09:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentHydratedHighlyStructuredMessage(t *testing.T) {
	msg := &waProto.Message{
		HighlyStructuredMessage: &waProto.HighlyStructuredMessage{
			HydratedHsm: &waProto.TemplateMessage{
				HydratedTemplate: &waProto.TemplateMessage_HydratedFourRowTemplate{
					HydratedContentText: proto.String("Haircut confirmed for 2030-01-15 at 09:00"),
				},
			},
		},
	}

	want := "Haircut confirmed for 2030-01-15 at 09:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentUnhydratedHighlyStructuredMessage(t *testing.T) {
	msg := &waProto.Message{
		HighlyStructuredMessage: &waProto.HighlyStructuredMessage{
			ElementName: proto.String("appointment_confirmation"),
			Params:      []string{"2030-01-15", "09:00"},
		},
	}

	want := "[business template: appointment_confirmation]\n2030-01-15\n09:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentClassicFourRowTemplate(t *testing.T) {
	msg := &waProto.Message{
		TemplateMessage: &waProto.TemplateMessage{
			Format: &waProto.TemplateMessage_FourRowTemplate_{
				FourRowTemplate: &waProto.TemplateMessage_FourRowTemplate{
					Content: &waProto.HighlyStructuredMessage{
						ElementName: proto.String("appointment_confirmation"),
						Params:      []string{"2030-01-15", "09:00"},
					},
					Footer: &waProto.HighlyStructuredMessage{Params: []string{"Arrive on time"}},
				},
			},
		},
	}

	want := "[business template: appointment_confirmation]\n2030-01-15\n09:00\nArrive on time"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestPopulatedMessageFieldsIdentifiesUnhandledEnvelope(t *testing.T) {
	msg := &waProto.Message{
		TemplateMessage:    &waProto.TemplateMessage{},
		InteractiveMessage: &waProto.InteractiveMessage{},
	}

	got := populatedMessageFields(msg)
	for _, want := range []string{"templateMessage", "interactiveMessage"} {
		if !slices.Contains(got, want) {
			t.Fatalf("populatedMessageFields() = %v, missing %q", got, want)
		}
	}
	if !slices.IsSorted(got) {
		t.Fatalf("populatedMessageFields() = %v, want sorted output", got)
	}
}

func TestExtractTextContentLocalizableParamsWithoutDefault(t *testing.T) {
	msg := &waProto.Message{
		HighlyStructuredMessage: &waProto.HighlyStructuredMessage{
			ElementName: proto.String("payment_due"),
			LocalizableParams: []*waProto.HighlyStructuredMessage_HSMLocalizableParameter{
				{
					ParamOneof: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_Currency{
						Currency: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMCurrency{
							CurrencyCode: proto.String("BRL"),
							Amount1000:   proto.Int64(150000),
						},
					},
				},
				{
					ParamOneof: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_DateTime{
						DateTime: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMDateTime{
							DatetimeOneof: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMDateTime_Component{
								Component: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMDateTime_HSMDateTimeComponent{
									Year:       proto.Uint32(2030),
									Month:      proto.Uint32(1),
									DayOfMonth: proto.Uint32(15),
									Hour:       proto.Uint32(9),
									Minute:     proto.Uint32(0),
								},
							},
						},
					},
				},
			},
		},
	}

	want := "[business template: payment_due]\nBRL 150.00\n2030-01-15 09:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentInteractiveCarousel(t *testing.T) {
	msg := &waProto.Message{
		InteractiveMessage: &waProto.InteractiveMessage{
			InteractiveMessage: &waProto.InteractiveMessage_CarouselMessage_{
				CarouselMessage: &waProto.InteractiveMessage_CarouselMessage{
					Cards: []*waProto.InteractiveMessage{
						{Body: &waProto.InteractiveMessage_Body{Text: proto.String("Option 1: 09:00")}},
						{Body: &waProto.InteractiveMessage_Body{Text: proto.String("Option 2: 14:00")}},
					},
				},
			},
		},
	}

	want := "Option 1: 09:00\nOption 2: 14:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentStopsOnCyclicTemplate(t *testing.T) {
	template := &waProto.TemplateMessage{}
	hsm := &waProto.HighlyStructuredMessage{HydratedHsm: template}
	template.Format = &waProto.TemplateMessage_FourRowTemplate_{
		FourRowTemplate: &waProto.TemplateMessage_FourRowTemplate{Content: hsm},
	}

	// Must terminate; the exact placeholder text is not important.
	_ = extractTextContent(&waProto.Message{TemplateMessage: template})
}

func TestExtractTextContentOtherMessageTypes(t *testing.T) {
	tests := []struct {
		name string
		msg  *waProto.Message
		want string
	}{
		{
			name: "image caption",
			msg:  &waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String("Comprovante do pagamento")}},
			want: "Comprovante do pagamento",
		},
		{
			name: "video caption",
			msg:  &waProto.Message{VideoMessage: &waProto.VideoMessage{Caption: proto.String("Vídeo da entrega")}},
			want: "Vídeo da entrega",
		},
		{
			name: "document caption",
			msg:  &waProto.Message{DocumentMessage: &waProto.DocumentMessage{Caption: proto.String("Segue o contrato")}},
			want: "Segue o contrato",
		},
		{
			name: "buttons message",
			msg: &waProto.Message{ButtonsMessage: &waProto.ButtonsMessage{
				ContentText: proto.String("Confirma sua consulta amanhã às 09:00?"),
				FooterText:  proto.String("Clínica Exemplo"),
				Buttons: []*waProto.ButtonsMessage_Button{
					{ButtonText: &waProto.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Confirmar")}},
					{ButtonText: &waProto.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Remarcar")}},
				},
			}},
			want: "Confirma sua consulta amanhã às 09:00?\nClínica Exemplo\n[button: Confirmar]\n[button: Remarcar]",
		},
		{
			name: "list message",
			msg: &waProto.Message{ListMessage: &waProto.ListMessage{
				Title:       proto.String("Horários disponíveis"),
				Description: proto.String("Escolha um horário"),
				Sections: []*waProto.ListMessage_Section{{
					Title: proto.String("Terça"),
					Rows: []*waProto.ListMessage_Row{
						{Title: proto.String("09:00"), Description: proto.String("Dr. Silva")},
						{Title: proto.String("14:00")},
					},
				}},
			}},
			want: "Horários disponíveis\nEscolha um horário\nTerça\n- 09:00: Dr. Silva\n- 14:00",
		},
		{
			name: "buttons response",
			msg:  &waProto.Message{ButtonsResponseMessage: &waProto.ButtonsResponseMessage{SelectedButtonID: proto.String("confirm"), Response: &waProto.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Confirmar"}}},
			want: "Confirmar",
		},
		{
			name: "list response",
			msg:  &waProto.Message{ListResponseMessage: &waProto.ListResponseMessage{Title: proto.String("09:00"), Description: proto.String("Dr. Silva")}},
			want: "09:00\nDr. Silva",
		},
		{
			name: "list response without title",
			msg: &waProto.Message{ListResponseMessage: &waProto.ListResponseMessage{
				SingleSelectReply: &waProto.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("slot-0900")},
			}},
			want: "[list reply: slot-0900]",
		},
		{
			name: "template button reply",
			msg:  &waProto.Message{TemplateButtonReplyMessage: &waProto.TemplateButtonReplyMessage{SelectedID: proto.String("cancel"), SelectedDisplayText: proto.String("Cancelar")}},
			want: "Cancelar",
		},
		{
			name: "interactive response",
			msg: &waProto.Message{InteractiveResponseMessage: &waProto.InteractiveResponseMessage{
				Body: &waProto.InteractiveResponseMessage_Body{Text: proto.String("Quero falar com um atendente")},
			}},
			want: "Quero falar com um atendente",
		},
		{
			name: "location",
			msg: &waProto.Message{LocationMessage: &waProto.LocationMessage{
				Name:             proto.String("Escritório"),
				Address:          proto.String("Av. Paulista, 1000"),
				DegreesLatitude:  proto.Float64(-23.561414),
				DegreesLongitude: proto.Float64(-46.655881),
			}},
			want: "[location: Escritório]\nAv. Paulista, 1000\nhttps://maps.google.com/?q=-23.561414,-46.655881",
		},
		{
			name: "live location",
			msg: &waProto.Message{LiveLocationMessage: &waProto.LiveLocationMessage{
				Caption:          proto.String("Chegando"),
				DegreesLatitude:  proto.Float64(-23.5),
				DegreesLongitude: proto.Float64(-46.6),
			}},
			want: "[live location]\nChegando\nhttps://maps.google.com/?q=-23.500000,-46.600000",
		},
		{
			name: "contact",
			msg: &waProto.Message{ContactMessage: &waProto.ContactMessage{
				DisplayName: proto.String("Maria Souza"),
				Vcard:       proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Maria Souza\nitem1.TEL;waid=5511999999999:+55 11 99999-9999\nTEL;type=WORK:+55 11 3333-3333\nEND:VCARD"),
			}},
			want: "[contact]\nMaria Souza: +55 11 99999-9999, +55 11 3333-3333",
		},
		{
			name: "contacts array",
			msg: &waProto.Message{ContactsArrayMessage: &waProto.ContactsArrayMessage{
				DisplayName: proto.String("2 contatos"),
				Contacts: []*waProto.ContactMessage{
					{DisplayName: proto.String("Ana"), Vcard: proto.String("BEGIN:VCARD\r\nTEL:+55 11 1111-1111\r\nEND:VCARD")},
					{DisplayName: proto.String("Bruno")},
				},
			}},
			want: "[contacts: 2 contatos]\nAna: +55 11 1111-1111\nBruno",
		},
		{
			name: "poll",
			msg: &waProto.Message{PollCreationMessageV3: &waProto.PollCreationMessage{
				Name:    proto.String("Almoço de sexta?"),
				Options: []*waProto.PollCreationMessage_Option{{OptionName: proto.String("Sim")}, {OptionName: proto.String("Não")}},
			}},
			want: "[poll: Almoço de sexta?]\n- Sim\n- Não",
		},
		{
			name: "group invite",
			msg:  &waProto.Message{GroupInviteMessage: &waProto.GroupInviteMessage{GroupName: proto.String("Time de Produto"), Caption: proto.String("Entra aí")}},
			want: "[group invite: Time de Produto]\nEntra aí",
		},
		{
			name: "hydrated template buttons",
			msg: &waProto.Message{TemplateMessage: &waProto.TemplateMessage{
				HydratedTemplate: &waProto.TemplateMessage_HydratedFourRowTemplate{
					HydratedContentText: proto.String("Sua fatura vence amanhã"),
					HydratedButtons: []*waProto.HydratedTemplateButton{
						{HydratedButton: &waProto.HydratedTemplateButton_UrlButton{UrlButton: &waProto.HydratedTemplateButton_HydratedURLButton{
							DisplayText: proto.String("Pagar"), URL: proto.String("https://example.com/pay"),
						}}},
						{HydratedButton: &waProto.HydratedTemplateButton_CallButton{CallButton: &waProto.HydratedTemplateButton_HydratedCallButton{
							DisplayText: proto.String("Ligar"), PhoneNumber: proto.String("+551130000000"),
						}}},
						{HydratedButton: &waProto.HydratedTemplateButton_QuickReplyButton{QuickReplyButton: &waProto.HydratedTemplateButton_HydratedQuickReplyButton{
							DisplayText: proto.String("Já paguei"),
						}}},
					},
				},
			}},
			want: "Sua fatura vence amanhã\n[button: Pagar → https://example.com/pay]\n[button: Ligar → +551130000000]\n[button: Já paguei]",
		},
		{
			name: "interactive native flow buttons",
			msg: &waProto.Message{InteractiveMessage: &waProto.InteractiveMessage{
				Body: &waProto.InteractiveMessage_Body{Text: proto.String("Pague com PIX")},
				InteractiveMessage: &waProto.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waProto.InteractiveMessage_NativeFlowMessage{
					Buttons: []*waProto.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
						{Name: proto.String("cta_copy"), ButtonParamsJSON: proto.String(`{"display_text":"Copiar código","copy_code":"00020126PIX"}`)},
						{Name: proto.String("cta_url"), ButtonParamsJSON: proto.String(`{"display_text":"Ver pedido","url":"https://example.com/order"}`)},
						{Name: proto.String("broken"), ButtonParamsJSON: proto.String(`not json`)},
					},
				}},
			}},
			want: "Pague com PIX\n[button: Copiar código → 00020126PIX]\n[button: Ver pedido → https://example.com/order]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextContent(tt.msg); got != tt.want {
				t.Fatalf("extractTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractTextContentUnwrapsEnvelopes(t *testing.T) {
	template := &waProto.Message{TemplateMessage: &waProto.TemplateMessage{
		HydratedTemplate: &waProto.TemplateMessage_HydratedFourRowTemplate{
			HydratedContentText: proto.String("Consulta confirmada"),
		},
	}}
	// History sync hands over raw envelopes: a business template sent as view
	// once inside a chat with disappearing messages.
	msg := &waProto.Message{EphemeralMessage: &waProto.FutureProofMessage{
		Message: &waProto.Message{ViewOnceMessageV2: &waProto.FutureProofMessage{Message: template}},
	}}

	if got, want := extractTextContent(msg), "Consulta confirmada"; got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}

	document := &waProto.Message{DocumentWithCaptionMessage: &waProto.FutureProofMessage{
		Message: &waProto.Message{DocumentMessage: &waProto.DocumentMessage{
			FileName: proto.String("boleto.pdf"),
			Caption:  proto.String("Boleto de outubro"),
		}},
	}}
	if got, want := extractTextContent(document), "Boleto de outubro"; got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
	if mediaType, filename, _, _, _, _, _ := extractMediaInfo(document); mediaType != "document" || filename != "boleto.pdf" {
		t.Fatalf("extractMediaInfo() = %q, %q, want document, boleto.pdf", mediaType, filename)
	}
}

func TestExtractTextContentEventUsesLocalTime(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("BRT", -3*60*60)
	defer func() { time.Local = previous }()

	start := time.Date(2030, 1, 15, 9, 0, 0, 0, time.Local)
	msg := &waProto.Message{EventMessage: &waProto.EventMessage{
		Name:        proto.String("Planejamento Q1"),
		Description: proto.String("Revisão de metas"),
		StartTime:   proto.Int64(start.Unix()),
		EndTime:     proto.Int64(start.Add(time.Hour).Unix()),
		Location:    &waProto.LocationMessage{Name: proto.String("Sala 3")},
	}}

	want := "[event: Planejamento Q1]\nRevisão de metas\n2030-01-15 09:00 -03:00 to 2030-01-15 10:00 -03:00\nSala 3"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractTextContentLocalizableEpochUsesLocalTime(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("BRT", -3*60*60)
	defer func() { time.Local = previous }()

	appointment := time.Date(2030, 1, 15, 9, 0, 0, 0, time.Local)
	msg := &waProto.Message{HighlyStructuredMessage: &waProto.HighlyStructuredMessage{
		ElementName: proto.String("appointment_reminder"),
		LocalizableParams: []*waProto.HighlyStructuredMessage_HSMLocalizableParameter{{
			ParamOneof: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_DateTime{
				DateTime: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMDateTime{
					DatetimeOneof: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMDateTime_UnixEpoch{
						UnixEpoch: &waProto.HighlyStructuredMessage_HSMLocalizableParameter_HSMDateTime_HSMDateTimeUnixEpoch{
							Timestamp: proto.Int64(appointment.Unix()),
						},
					},
				},
			},
		}},
	}}

	want := "[business template: appointment_reminder]\n2030-01-15 09:00 -03:00"
	if got := extractTextContent(msg); got != want {
		t.Fatalf("extractTextContent() = %q, want %q", got, want)
	}
}

func TestExtractMediaInfoOtherMediaTypes(t *testing.T) {
	tests := []struct {
		name          string
		msg           *waProto.Message
		wantMediaType string
		wantURL       string
	}{
		{
			name:          "sticker",
			msg:           &waProto.Message{StickerMessage: &waProto.StickerMessage{URL: proto.String("https://mmg.whatsapp.net/sticker")}},
			wantMediaType: "sticker",
			wantURL:       "https://mmg.whatsapp.net/sticker",
		},
		{
			name:          "round video note",
			msg:           &waProto.Message{PtvMessage: &waProto.VideoMessage{URL: proto.String("https://mmg.whatsapp.net/ptv")}},
			wantMediaType: "video",
			wantURL:       "https://mmg.whatsapp.net/ptv",
		},
		{
			name: "document in template header",
			msg: &waProto.Message{TemplateMessage: &waProto.TemplateMessage{
				HydratedTemplate: &waProto.TemplateMessage_HydratedFourRowTemplate{
					Title: &waProto.TemplateMessage_HydratedFourRowTemplate_DocumentMessage{
						DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("fatura.pdf"), URL: proto.String("https://mmg.whatsapp.net/invoice")},
					},
					HydratedContentText: proto.String("Sua fatura chegou"),
				},
			}},
			wantMediaType: "document",
			wantURL:       "https://mmg.whatsapp.net/invoice",
		},
		{
			name: "image in interactive header",
			msg: &waProto.Message{InteractiveMessage: &waProto.InteractiveMessage{
				Header: &waProto.InteractiveMessage_Header{
					Media: &waProto.InteractiveMessage_Header_ImageMessage{ImageMessage: &waProto.ImageMessage{URL: proto.String("https://mmg.whatsapp.net/banner")}},
				},
				Body: &waProto.InteractiveMessage_Body{Text: proto.String("Promoção")},
			}},
			wantMediaType: "image",
			wantURL:       "https://mmg.whatsapp.net/banner",
		},
		{
			name: "image in buttons message",
			msg: &waProto.Message{ButtonsMessage: &waProto.ButtonsMessage{
				Header:      &waProto.ButtonsMessage_ImageMessage{ImageMessage: &waProto.ImageMessage{URL: proto.String("https://mmg.whatsapp.net/header")}},
				ContentText: proto.String("Confira"),
			}},
			wantMediaType: "image",
			wantURL:       "https://mmg.whatsapp.net/header",
		},
		{
			name:          "plain text has no media",
			msg:           &waProto.Message{Conversation: proto.String("oi")},
			wantMediaType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaType, _, url, _, _, _, _ := extractMediaInfo(tt.msg)
			if mediaType != tt.wantMediaType || url != tt.wantURL {
				t.Fatalf("extractMediaInfo() = %q, %q, want %q, %q", mediaType, url, tt.wantMediaType, tt.wantURL)
			}
		})
	}
}

type recordingLogger struct {
	warnings []string
	debug    []string
}

func (l *recordingLogger) Warnf(msg string, args ...interface{}) {
	l.warnings = append(l.warnings, msg)
}
func (l *recordingLogger) Errorf(msg string, args ...interface{}) {}
func (l *recordingLogger) Infof(msg string, args ...interface{})  {}
func (l *recordingLogger) Debugf(msg string, args ...interface{}) { l.debug = append(l.debug, msg) }
func (l *recordingLogger) Sub(module string) waLog.Logger         { return l }

func TestLogDroppedMessageOnlyWarnsForUnexpectedEnvelopes(t *testing.T) {
	logger := &recordingLogger{}

	logDroppedMessage(logger, "reaction", "chat", &waProto.Message{
		ReactionMessage:    &waProto.ReactionMessage{Text: proto.String("👍")},
		MessageContextInfo: &waProto.MessageContextInfo{},
	})
	logDroppedMessage(logger, "revoke", "chat", &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{}})
	if len(logger.warnings) != 0 || len(logger.debug) != 2 {
		t.Fatalf("expected envelopes: warnings=%d debug=%d, want 0 and 2", len(logger.warnings), len(logger.debug))
	}

	logDroppedMessage(logger, "order", "chat", &waProto.Message{OrderMessage: &waProto.OrderMessage{}})
	if len(logger.warnings) != 1 {
		t.Fatalf("unexpected envelope: warnings=%d, want 1", len(logger.warnings))
	}
}

func TestDroppedMessageTallySummarizesUnexpectedEnvelopes(t *testing.T) {
	logger := &recordingLogger{}
	tally := droppedMessageTally{}

	tally.add(logger, "1", "chat", &waProto.Message{OrderMessage: &waProto.OrderMessage{}})
	tally.add(logger, "2", "chat", &waProto.Message{OrderMessage: &waProto.OrderMessage{}})
	tally.add(logger, "3", "chat", &waProto.Message{CallLogMesssage: &waProto.CallLogMessage{}})
	tally.add(logger, "4", "chat", &waProto.Message{ReactionMessage: &waProto.ReactionMessage{}})

	summary := tally.summary()
	for _, want := range []string{"[orderMessage]=2", "[callLogMesssage]=1"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary() = %q, missing %q", summary, want)
		}
	}
	if strings.Contains(summary, "reactionMessage") {
		t.Fatalf("summary() = %q, should not count expected envelopes", summary)
	}
	if len(logger.warnings) != 0 {
		t.Fatalf("tally.add() logged %d warnings, want 0 (one summary at the end instead)", len(logger.warnings))
	}
}
