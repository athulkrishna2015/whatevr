package views

import (
	"go.mau.fi/whatsmeow/proto/waE2E"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/model"
	"whatevrd/internal/store"
)

// body puts the arm of the row's kind on it.
func (c *rc) body(row *v2.MessageRow, ch chatCtx, m model.Message, sm store.Message, raw *waE2E.Message) {
	p := store.DecodePayload(sm.PayloadJSON)
	media := func() *v2.Media {
		if !store.MessageCarriesMedia(sm) {
			return nil
		}
		md := v2.Media_builder{
			Mime:          sm.MediaMimeType,
			Width:         uint32(max(sm.MediaWidth, 0)),
			Height:        uint32(max(sm.MediaHeight, 0)),
			SizeBytes:     uint64(max(sm.MediaSizeBytes, 0)),
			DurationMs:    int64(sm.MediaDurationSecs) * 1000,
			ThumbnailPath: sm.MediaThumbnailLocalPath,
			Path:          sm.MediaLocalPath,
			DownloadError: sm.MediaDownloadError,
		}.Build()
		if sm.MediaLocalPath == "" && c.transferring(m) {
			md.SetDownloading(true)
		}
		return md
	}
	switch sm.MediaKind {
	case "":
		t := &v2.Text{}
		if lp := p.LinkPreview; lp != nil {
			t.SetLinkPreview(v2.LinkPreview_builder{
				Url: lp.URL, Host: lp.Host, Title: lp.Title, Description: lp.Description, Type: linkType(lp.Type),
				ThumbnailPath: lp.ThumbnailPath, ThumbnailWidth: uint32(max(lp.ThumbnailWidth, 0)),
				ThumbnailHeight: uint32(max(lp.ThumbnailHeight, 0)),
			}.Build())
		}
		row.SetTextBody(t)
	case store.MediaKindImage:
		row.SetImage(v2.Image_builder{Media: media()}.Build())
	case store.MediaKindVideo:
		row.SetVideo(v2.Video_builder{Media: media()}.Build())
	case store.MediaKindGIF:
		row.SetGif(v2.Gif_builder{Media: media()}.Build())
	case store.MediaKindVoice:
		row.SetVoice(v2.Voice_builder{Media: media(), Waveform: sm.MediaWaveform, Played: sm.MediaPlayed}.Build())
	case store.MediaKindAudio:
		row.SetAudio(v2.Audio_builder{Media: media()}.Build())
	case store.MediaKindDocument:
		row.SetDocument(v2.Document_builder{Media: media(), Filename: sm.MediaFileName,
			PageCount: uint32(max(sm.MediaPageCount, 0))}.Build())
	case store.MediaKindVideoNote:
		row.SetVideoNote(v2.VideoNote_builder{Media: media()}.Build())
	case store.MediaKindSticker:
		lottie := raw != nil && model.Unwrap(raw).Msg.GetStickerMessage().GetIsLottie()
		row.SetSticker(v2.Sticker_builder{Media: media(), StickerId: sm.MediaCacheKey, Animated: sm.MediaAnimated,
			Lottie: lottie}.Build())
	case store.MediaKindLocation:
		l := location(p.Location)
		if l == nil {
			l = &v2.Location{}
		}
		l.SetMap(media())
		row.SetLocation(l)
	case store.MediaKindLiveLocation:
		l := location(p.Location)
		if l == nil {
			l = &v2.Location{}
		}
		l.SetMap(media())
		ll := v2.LiveLocation_builder{Location: l}.Build()
		if s := p.LiveShare; s != nil {
			ll.SetActive(s.Active)
			ll.SetStartedMs(s.StartedAt * 1000)
			ll.SetExpiresMs(s.ExpiresAt * 1000)
			ll.SetUpdatedMs(s.UpdatedAt * 1000)
			ll.SetSpeedMps(s.SpeedMPS)
			ll.SetPointCount(uint32(max(s.PointCount, 0)))
			if s.HeadingDegrees >= 0 && s.HeadingDegrees < 360 && (s.HeadingDegrees != 0 || s.SpeedMPS > 0) {
				ll.SetHeadingDeg(uint32(s.HeadingDegrees))
			}
		}
		row.SetLiveLocation(ll)
	case store.MediaKindContact, store.MediaKindContacts:
		row.SetContacts(c.contacts(p.Contacts))
	case store.MediaKindPoll:
		poll := &v2.Poll{}
		if pp := p.Poll; pp != nil {
			poll.SetQuestion(pp.Question)
			poll.SetSelectable(uint32(max(pp.SelectableCount, 0)))
			poll.SetQuiz(pp.Quiz)
			poll.SetAllowAddOption(pp.AllowAddOption)
			poll.SetEndsMs(toMS(pp.EndsAt))
		}
		if raw != nil {
			if pc := pollOf(model.Unwrap(raw).Msg); pc != nil {
				c.tally(poll, pc, m.Facts.Votes)
			}
		}
		row.SetPoll(poll)
	case store.MediaKindGroupInvite:
		row.SetGroupInvite(c.invite(p.GroupInvite))
	case store.MediaKindEvent:
		ev := &v2.ScheduledEvent{}
		if e := p.Event; e != nil {
			ev.SetName(e.Name)
			ev.SetDescription(e.Description)
			ev.SetStartsMs(toMS(e.StartsAt))
			ev.SetEndsMs(toMS(e.EndsAt))
			ev.SetCanceled(e.Canceled)
			ev.SetJoinLink(e.JoinLink)
			if l := location(e.Location); l != nil {
				l.SetMap(media())
				ev.SetLocation(l)
			}
			ev.SetExtraGuestsAllowed(e.ExtraGuestsAllowed)
			ev.SetCall(e.ScheduleCall)
			ev.SetReminderOffsetMs(e.ReminderOffsetSecs * 1000)
		}
		c.rsvps(ev, m.Facts.Events)
		row.SetEvent(ev)
	case store.MediaKindAlbum:
		row.SetAlbum(c.album(ch, m, p.Album))
	case store.MediaKindInteractive:
		row.SetInteractive(interactive(p.Interactive))
	case store.MediaKindProduct, store.MediaKindOrder, store.MediaKindPayment:
		c.commerce(row, p.Commerce)
	case store.MediaKindStickerPack:
		row.SetStickerPack(c.stickerPack(p.StickerPack))
	case store.MediaKindCallLog:
		cl := &v2.CallLog{}
		if l := p.CallLog; l != nil {
			cl.SetVideo(l.Video)
			cl.SetOutcome(callOutcome(l.Outcome))
			cl.SetDurationMs(l.DurationSecs * 1000)
			cl.SetGroup(l.Group)
			cl.SetScheduled(l.Scheduled)
			cl.SetVoiceChat(l.VoiceChat)
			cl.SetParticipants(uint32(max(l.Participants, 0)))
		}
		row.SetCallLog(cl)
	case store.MediaKindWaiting:
		w := v2.Waiting_builder{FirstSeenMs: m.T, Never: m.Wait, Requests: uint32(max(m.Facts.Local.Asked, 0)), AskedPhone: m.Facts.Local.Asked > 0}.Build()
		if wp := p.Waiting; wp != nil {
			w.SetRetryAtMs(toMS(wp.RetryAt))
			w.SetRequests(uint32(max(wp.Requests, 0)))
			w.SetAskedPhone(wp.Asked)
		}
		row.SetWaiting(w)
	default:
		row.SetUnsupported(&v2.Unsupported{})
	}
}

// transferring says a download of m runs now.
func (c *rc) transferring(m model.Message) bool {
	if c.live == nil {
		return false
	}
	if t, ok := c.live.Transferring(m.Home, m.ID); ok && !t.Upload {
		return true
	}
	t, ok := c.live.Transferring(m.Chat, m.ID)
	return ok && !t.Upload
}

func location(l *store.LocationPayload) *v2.Location {
	if l == nil {
		return nil
	}
	return v2.Location_builder{Lat: l.Latitude, Lng: l.Longitude, Name: l.Name, Address: l.Address, Url: l.URL,
		AccuracyM: l.AccuracyMeters}.Build()
}

func linkType(t string) v2.LinkPreviewType {
	switch t {
	case "":
		return v2.LinkPreviewType_LINK_PREVIEW_TYPE_PAGE
	case "video":
		return v2.LinkPreviewType_LINK_PREVIEW_TYPE_VIDEO
	case "image":
		return v2.LinkPreviewType_LINK_PREVIEW_TYPE_IMAGE
	case "profile":
		return v2.LinkPreviewType_LINK_PREVIEW_TYPE_PROFILE
	case "payment_links":
		return v2.LinkPreviewType_LINK_PREVIEW_TYPE_PAYMENT_LINKS
	case "placeholder":
		return v2.LinkPreviewType_LINK_PREVIEW_TYPE_PLACEHOLDER
	}
	return v2.LinkPreviewType_LINK_PREVIEW_TYPE_UNSPECIFIED
}

func (c *rc) contacts(p *store.ContactsPayload) *v2.Contacts {
	out := &v2.Contacts{}
	if p == nil {
		return out
	}
	out.SetDisplayName(p.DisplayName)
	fields := func(fs []store.ContactField) []*v2.ContactField {
		var o []*v2.ContactField
		for _, f := range fs {
			cf := v2.ContactField_builder{Label: f.Label, Value: f.Value}.Build()
			if f.JID != "" {
				cf.SetPerson(c.now(f.JID))
			}
			o = append(o, cf)
		}
		return o
	}
	var cards []*v2.ContactCard
	for _, cd := range p.Cards {
		cards = append(cards, v2.ContactCard_builder{
			DisplayName: cd.DisplayName, Org: cd.Org, Title: cd.Title, Birthday: cd.Birthday,
			Phones: fields(cd.Phones), Emails: fields(cd.Emails), Urls: fields(cd.URLs), Addresses: fields(cd.Addresses),
			Vcard: cd.VCard,
		}.Build())
	}
	out.SetCards(cards)
	return out
}

func (c *rc) invite(g *store.GroupInvitePayload) *v2.GroupInvite {
	out := &v2.GroupInvite{}
	if g == nil {
		return out
	}
	out.SetCode(g.Code)
	out.SetExpiresMs(toMS(g.ExpiresAt))
	out.SetName(g.Name)
	out.SetCaption(g.Caption)
	out.SetThumbnailPath(g.PhotoPath)
	out.SetSubject(g.Subject)
	out.SetTopic(g.Topic)
	out.SetMemberCount(uint32(max(g.MemberCount, 0)))
	out.SetJoined(g.Joined)
	out.SetResolveError(g.ResolveError)
	if g.GroupJID != "" {
		c.wait(model.Norm(g.GroupJID), func(id, _ string) { out.SetChatId(id) })
	}
	return out
}

func (c *rc) album(ch chatCtx, m model.Message, p *store.AlbumPayload) *v2.Album {
	out := &v2.Album{}
	kids, err := c.r.AlbumChildren(c.ctx, m.Chat, m.ID)
	if err != nil {
		c.log.Warn().Err(err).Msg("views: album children")
	}
	items := make([]*v2.MessageRow, 0, len(kids))
	for _, k := range kids {
		items = append(items, c.message(ch, k))
	}
	out.SetItems(items)
	// an album that got all it promised describes itself by what it holds
	if e := p.Expected(); e > len(items) {
		out.SetExpected(uint32(e))
	}
	return out
}

func interactive(p *store.InteractivePayload) *v2.Interactive {
	out := &v2.Interactive{}
	if p == nil {
		return out
	}
	out.SetSource(interactiveSource(p.Source))
	out.SetTitle(p.Title)
	out.SetSubtitle(p.Subtitle)
	out.SetBody(p.Body)
	out.SetFooter(p.Footer)
	out.SetThumbnailPath(p.ThumbnailPath)
	out.SetDocumentName(p.DocumentName)
	out.SetListLabel(p.ListLabel)
	var buttons []*v2.InteractiveButton
	for _, b := range p.Buttons {
		buttons = append(buttons, v2.InteractiveButton_builder{Kind: buttonKind(b.Kind), Label: b.Label, Url: b.URL,
			Phone: b.Phone, Copy: b.Copy, Id: b.ID, Live: b.Live}.Build())
	}
	out.SetButtons(buttons)
	var sections []*v2.InteractiveSection
	for _, s := range p.Sections {
		var rows []*v2.InteractiveRow
		for _, r := range s.Rows {
			rows = append(rows, v2.InteractiveRow_builder{Id: r.ID, Title: r.Title, Description: r.Description}.Build())
		}
		sections = append(sections, v2.InteractiveSection_builder{Title: s.Title, Rows: rows}.Build())
	}
	out.SetSections(sections)
	var cards []*v2.Interactive
	for i := range p.Cards {
		cards = append(cards, interactive(&p.Cards[i]))
	}
	out.SetCards(cards)
	return out
}

func interactiveSource(s string) v2.InteractiveSource {
	switch s {
	case "buttons":
		return v2.InteractiveSource_INTERACTIVE_SOURCE_BUTTONS
	case "list":
		return v2.InteractiveSource_INTERACTIVE_SOURCE_LIST
	case "template":
		return v2.InteractiveSource_INTERACTIVE_SOURCE_TEMPLATE
	case "interactive":
		return v2.InteractiveSource_INTERACTIVE_SOURCE_INTERACTIVE
	case "carousel":
		return v2.InteractiveSource_INTERACTIVE_SOURCE_CAROUSEL
	}
	return v2.InteractiveSource_INTERACTIVE_SOURCE_UNSPECIFIED
}

func buttonKind(k string) v2.InteractiveButtonKind {
	switch k {
	case store.InteractiveButtonURL:
		return v2.InteractiveButtonKind_INTERACTIVE_BUTTON_KIND_URL
	case store.InteractiveButtonCall:
		return v2.InteractiveButtonKind_INTERACTIVE_BUTTON_KIND_CALL
	case store.InteractiveButtonCopy:
		return v2.InteractiveButtonKind_INTERACTIVE_BUTTON_KIND_COPY
	case store.InteractiveButtonReply:
		return v2.InteractiveButtonKind_INTERACTIVE_BUTTON_KIND_REPLY
	}
	return v2.InteractiveButtonKind_INTERACTIVE_BUTTON_KIND_OTHER
}

func money(thousandths int64, currency string) *v2.Money {
	if thousandths == 0 && currency == "" {
		return nil
	}
	return v2.Money_builder{Thousandths: thousandths, Currency: currency}.Build()
}

func (c *rc) commerce(row *v2.MessageRow, p *store.CommercePayload) {
	if p == nil {
		row.SetUnsupported(&v2.Unsupported{})
		return
	}
	seller := func() *v2.Person {
		if p.SellerJID == "" {
			return nil
		}
		return c.now(p.SellerJID)
	}
	switch p.Kind {
	case store.CommerceKindProduct:
		row.SetProduct(v2.Product_builder{Title: p.Title, Description: p.Description, Body: p.Body, Footer: p.Footer,
			ThumbnailPath: p.ThumbnailPath, Price: money(p.Amount1000, p.Currency),
			SalePrice: money(p.SalePrice1000, p.Currency), ProductId: p.ProductID, RetailerId: p.RetailerID,
			Url: p.URL, ImageCount: uint32(max(p.ImageCount, 0)), Seller: seller()}.Build())
	case store.CommerceKindOrder:
		st := v2.OrderStatus_ORDER_STATUS_UNSPECIFIED
		switch p.Status {
		case "inquiry":
			st = v2.OrderStatus_ORDER_STATUS_INQUIRY
		case "accepted":
			st = v2.OrderStatus_ORDER_STATUS_ACCEPTED
		case "declined":
			st = v2.OrderStatus_ORDER_STATUS_DECLINED
		}
		row.SetOrder(v2.Order_builder{Title: p.Title, ThumbnailPath: p.ThumbnailPath, OrderId: p.OrderID,
			ItemCount: uint32(max(p.ItemCount, 0)), Status: st, Total: money(p.Amount1000, p.Currency),
			Seller: seller()}.Build())
	default:
		kind := v2.PaymentKind_PAYMENT_KIND_UNSPECIFIED
		switch p.Kind {
		case store.CommerceKindPaymentRequest:
			kind = v2.PaymentKind_PAYMENT_KIND_REQUEST
		case store.CommerceKindPaymentSent:
			kind = v2.PaymentKind_PAYMENT_KIND_SENT
		case store.CommerceKindPaymentInvite:
			kind = v2.PaymentKind_PAYMENT_KIND_INVITE
		}
		pay := v2.Payment_builder{Kind: kind, Amount: money(p.Amount1000, p.Currency), Note: p.Note,
			ExpiresMs: toMS(p.ExpiresAt), Service: p.Service}.Build()
		if p.RequestedFrom != "" {
			pay.SetRequestedFrom(c.now(p.RequestedFrom))
		}
		row.SetPayment(pay)
	}
}

func (c *rc) stickerPack(p *store.StickerPackPayload) *v2.StickerPackShare {
	out := &v2.StickerPackShare{}
	if p == nil {
		return out
	}
	out.SetPackId(p.PackID)
	out.SetName(p.Name)
	out.SetPublisher(p.Publisher)
	out.SetDescription(p.Description)
	out.SetCaption(p.Caption)
	out.SetCount(uint32(max(p.Count, 0)))
	if p.PackID != "" {
		if pk, ok, err := c.r.GetStickerPack(c.ctx, p.PackID); err == nil && ok {
			out.SetInstallable(true)
			out.SetInstalled(pk.Installed)
		}
	}
	return out
}

func callOutcome(o string) v2.CallOutcome {
	switch o {
	case store.CallOutcomeConnected:
		return v2.CallOutcome_CALL_OUTCOME_CONNECTED
	case store.CallOutcomeMissed:
		return v2.CallOutcome_CALL_OUTCOME_MISSED
	case store.CallOutcomeFailed:
		return v2.CallOutcome_CALL_OUTCOME_FAILED
	case store.CallOutcomeRejected:
		return v2.CallOutcome_CALL_OUTCOME_REJECTED
	case store.CallOutcomeElsewhere:
		return v2.CallOutcome_CALL_OUTCOME_ACCEPTED_ELSEWHERE
	case store.CallOutcomeOngoing:
		return v2.CallOutcome_CALL_OUTCOME_ONGOING
	case store.CallOutcomeSilenced:
		return v2.CallOutcome_CALL_OUTCOME_SILENCED
	}
	return v2.CallOutcome_CALL_OUTCOME_UNSPECIFIED
}
