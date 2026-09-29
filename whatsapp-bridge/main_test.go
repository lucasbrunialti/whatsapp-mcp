package main

import (
	"slices"
	"testing"

	waProto "go.mau.fi/whatsmeow/binary/proto"
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

	want := "[business template: appointment_confirmation]\n2030-01-15\n09:00\n[business template]\nArrive on time"
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
