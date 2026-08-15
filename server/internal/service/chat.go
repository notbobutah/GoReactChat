// Package service adapts the chat session and store to the generated Connect
// handler interface. It is a translation layer only: no business logic lives
// here that isn't already covered by chat, orchestrator, or store.
package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	chatv1 "github.com/expona-ai/lumi-go/server/gen/lumi/chat/v1"
	"github.com/expona-ai/lumi-go/server/gen/lumi/chat/v1/chatv1connect"
	"github.com/expona-ai/lumi-go/server/internal/auth"
	"github.com/expona-ai/lumi-go/server/internal/chat"
	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
	"github.com/expona-ai/lumi-go/server/internal/store"
)

// ChatService implements chatv1connect.ChatServiceHandler.
type ChatService struct {
	store   store.Store
	session *chat.Session
	logger  *slog.Logger
}

func NewChatService(st store.Store, session *chat.Session, logger *slog.Logger) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChatService{store: st, session: session, logger: logger}
}

var _ chatv1connect.ChatServiceHandler = (*ChatService)(nil)

// scopeOf pulls the verified tenant scope off the context. A missing identity
// means the interceptor was not wired — an internal error, not a 401.
func scopeOf(ctx context.Context) (store.Scope, error) {
	id, ok := auth.FromContext(ctx)
	if !ok {
		return store.Scope{}, connect.NewError(connect.CodeInternal, errors.New("no identity on context"))
	}
	return id.Scope(), nil
}

// storeErr maps store errors onto Connect codes. ErrNotFound covers both
// "absent" and "belongs to another tenant" — deliberately indistinguishable.
func storeErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, errors.New("conversation not found"))
	}
	return connect.NewError(connect.CodeInternal, err)
}

// SendMessage runs one turn, streaming each surface event to the client.
func (s *ChatService) SendMessage(
	ctx context.Context,
	req *connect.Request[chatv1.SendMessageRequest],
	stream *connect.ServerStream[chatv1.ChatEvent],
) error {
	scope, err := scopeOf(ctx)
	if err != nil {
		return err
	}

	msg := req.Msg
	if strings.TrimSpace(msg.GetConversationId()) == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("conversation_id is required"))
	}
	// Text may be empty only when the turn carries attachments — the document
	// IS the message in that case.
	if strings.TrimSpace(msg.GetText()) == "" && len(msg.GetAttachments()) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("text or attachments required"))
	}

	err = s.session.Send(ctx, scope, chat.SendInput{
		ConversationID: msg.GetConversationId(),
		Text:           msg.GetText(),
		ProjectID:      msg.GetProjectId(),
	}, func(ev chat.Event) error {
		return stream.Send(toProtoEvent(ev))
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// The client hung up mid-turn. Not an error worth reporting.
			return nil
		}
		s.logger.Error("send message failed",
			"conversation_id", msg.GetConversationId(), "error", err)
		return storeErr(err)
	}
	return nil
}

func toProtoEvent(ev chat.Event) *chatv1.ChatEvent {
	switch ev.Type {
	case chat.EventToken:
		return &chatv1.ChatEvent{Event: &chatv1.ChatEvent_Token{
			Token: &chatv1.TokenEvent{Content: ev.Content, Rendered: ev.Rendered},
		}}
	case chat.EventBlock:
		return &chatv1.ChatEvent{Event: &chatv1.ChatEvent_Block{Block: toProtoBlock(ev.Block, ev.Rendered)}}
	case chat.EventDiscardBuffer:
		return &chatv1.ChatEvent{Event: &chatv1.ChatEvent_DiscardBuffer{
			DiscardBuffer: &chatv1.DiscardBufferEvent{},
		}}
	case chat.EventConversationTitled:
		return &chatv1.ChatEvent{Event: &chatv1.ChatEvent_ConversationTitled{
			ConversationTitled: &chatv1.ConversationTitledEvent{
				ConversationId: ev.ConversationID, Title: ev.Title,
			},
		}}
	case chat.EventError:
		return &chatv1.ChatEvent{Event: &chatv1.ChatEvent_Error{
			Error: &chatv1.ErrorEvent{Code: ev.Code, Message: ev.Message},
		}}
	default:
		return &chatv1.ChatEvent{Event: &chatv1.ChatEvent_Done{Done: &chatv1.DoneEvent{}}}
	}
}

func toProtoBlock(b *orchestrator.CanvasBlock, rendered string) *chatv1.BlockEvent {
	if b == nil {
		return &chatv1.BlockEvent{Rendered: rendered}
	}
	out := &chatv1.BlockEvent{Kind: b.Kind, Id: b.ID, Rendered: b.Rendered}
	if out.Rendered == "" {
		out.Rendered = rendered
	}
	if len(b.Data) > 0 {
		// A block whose payload won't marshal still renders — the markdown is
		// the load-bearing half — so drop the data rather than failing the turn.
		if data, err := structpb.NewStruct(b.Data); err == nil {
			out.Data = data
		}
	}
	return out
}

func (s *ChatService) ListConversations(
	ctx context.Context,
	req *connect.Request[chatv1.ListConversationsRequest],
) (*connect.Response[chatv1.ListConversationsResponse], error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}

	convs, err := s.store.ListConversations(ctx, scope, int(req.Msg.GetLimit()), req.Msg.GetProjectId())
	if err != nil {
		return nil, storeErr(err)
	}

	out := make([]*chatv1.Conversation, 0, len(convs))
	for i := range convs {
		out = append(out, toProtoConversation(&convs[i]))
	}
	return connect.NewResponse(&chatv1.ListConversationsResponse{Conversations: out}), nil
}

func (s *ChatService) GetMessages(
	ctx context.Context,
	req *connect.Request[chatv1.GetMessagesRequest],
) (*connect.Response[chatv1.GetMessagesResponse], error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}

	msgs, err := s.store.GetMessages(ctx, req.Msg.GetConversationId(), scope)
	if err != nil {
		return nil, storeErr(err)
	}

	out := make([]*chatv1.Message, 0, len(msgs))
	for i := range msgs {
		m := &msgs[i]
		blocks := make([]*chatv1.BlockEvent, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			blocks = append(blocks, toProtoBlock(&orchestrator.CanvasBlock{
				Kind: b.Kind, ID: b.ID, Rendered: b.Rendered, Data: b.Data,
			}, b.Rendered))
		}
		out = append(out, &chatv1.Message{
			Id:        m.ID,
			Role:      m.Role,
			Content:   m.Content,
			Blocks:    blocks,
			CreatedAt: timestamppb.New(m.CreatedAt),
		})
	}
	return connect.NewResponse(&chatv1.GetMessagesResponse{Messages: out}), nil
}

func (s *ChatService) RenameConversation(
	ctx context.Context,
	req *connect.Request[chatv1.RenameConversationRequest],
) (*connect.Response[chatv1.RenameConversationResponse], error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Msg.GetTitle())
	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title is required"))
	}

	conv, err := s.store.UpdateConversation(ctx, req.Msg.GetConversationId(),
		store.ConversationPatch{Title: &title}, scope)
	if err != nil {
		return nil, storeErr(err)
	}
	return connect.NewResponse(&chatv1.RenameConversationResponse{
		Conversation: toProtoConversation(conv),
	}), nil
}

func (s *ChatService) ArchiveConversation(
	ctx context.Context,
	req *connect.Request[chatv1.ArchiveConversationRequest],
) (*connect.Response[chatv1.ArchiveConversationResponse], error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	archived := "archived"
	if _, err := s.store.UpdateConversation(ctx, req.Msg.GetConversationId(),
		store.ConversationPatch{Status: &archived}, scope); err != nil {
		return nil, storeErr(err)
	}
	return connect.NewResponse(&chatv1.ArchiveConversationResponse{}), nil
}

func (s *ChatService) DeleteConversation(
	ctx context.Context,
	req *connect.Request[chatv1.DeleteConversationRequest],
) (*connect.Response[chatv1.DeleteConversationResponse], error) {
	scope, err := scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	deleted, err := s.store.DeleteConversation(ctx, req.Msg.GetConversationId(), scope)
	if err != nil {
		return nil, storeErr(err)
	}
	return connect.NewResponse(&chatv1.DeleteConversationResponse{
		DeletedMessages: int32(deleted),
	}), nil
}

func toProtoConversation(c *store.Conversation) *chatv1.Conversation {
	if c == nil {
		return nil
	}
	out := &chatv1.Conversation{
		Id:           c.ID,
		Title:        c.Title,
		Status:       c.Status,
		MessageCount: int32(c.MessageCount),
		ProjectId:    c.ProjectID,
		CreatedAt:    timestamppb.New(c.CreatedAt),
		UpdatedAt:    timestamppb.New(c.UpdatedAt),
	}
	if !c.LastMessageAt.IsZero() {
		out.LastMessageAt = timestamppb.New(c.LastMessageAt)
	}
	return out
}
