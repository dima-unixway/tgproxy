package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tgproxy/router"
	"tgproxy/telegram"
	"tgproxy/types"

	"github.com/gorilla/mux"
)

type Handlers struct {
	tg     *telegram.Client
	log    *slog.Logger
	router *router.Router
}

func NewHandlers(tg *telegram.Client, log *slog.Logger, router *router.Router) *Handlers {
	return &Handlers{
		tg:     tg,
		log:    log,
		router: router,
	}
}

func (h *Handlers) SendMessage(w http.ResponseWriter, r *http.Request) {
	var msg *types.Message

	bytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("Failed to read request", "error", err)
		respondError(w, http.StatusBadRequest, "Failed to read request")
		return
	}

	h.log.Debug("parsed bytes", "bytes", bytes)

	if msg, err = types.UnmarshalJSON(bytes); err != nil {
		h.log.Error("Failed to decode send request", "error", err)
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err = h.tg.SendMessage(msg)
	if err != nil {
		h.log.Error("Failed to send message",
			"peer_id", msg.PeerID,
			"error", err)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.log.Info("Send message successful",
		"peer_id", msg.PeerID)

	respondJSON(w, http.StatusOK, types.SendResponse{
		Success: true,
	})
}

func (h *Handlers) GetPeers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	query := vars["query"]
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		limitStr = "5"
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		h.log.Warn("Invalid limit", "limit", limitStr, "error", err)
		respondError(w, http.StatusBadRequest, "Invalid limit parameter")
		return
	}

	h.log.Info("Get peers request received", "query", query, "limit", limit)

	peers, err := h.tg.GetPeers(query, limit)
	if err != nil {
		h.log.Error("Failed to get peers", "query", query, "limit", limit, "error", err)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.log.Info("Get peers successful", "count", len(peers))

	respondJSON(w, http.StatusOK, types.PeersResponse{
		Peers: peers,
	})
}

func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

func (h *Handlers) MessagesHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chatIDStr := vars["chats"]
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		limitStr = "1"
	}

	chatIDs := strings.Split(chatIDStr, ",")
	var cleanChatIDs []string
	for _, id := range chatIDs {
		cleanID := strings.TrimSpace(id)
		if cleanID != "" {
			cleanChatIDs = append(cleanChatIDs, cleanID)
		}
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 1
		http.Error(w, "Invalid limit: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.tg.GetMessages(cleanChatIDs, limit)
	if err != nil {
		h.log.Error("Failed to get messages", "chat_ids", cleanChatIDs, "limit", limit, "error", err)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.log.Info("Get messages successful", "chat_ids", len(cleanChatIDs), "limit", limit, "message_count", len(resp.Messages))

	respondJSON(w, http.StatusOK, resp)
}

func (h *Handlers) SubscribeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chatsStr := vars["chats"]
	chats := strings.Split(chatsStr, ",")
	var cleanChatIDs []string
	for _, id := range chats {
		cleanID := strings.TrimSpace(id)
		if cleanID != "" {
			cleanChatIDs = append(cleanChatIDs, cleanID)
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	h.log.Info("Subscribe handler started", "chats", cleanChatIDs)

	// Hijack the connection to set infinite WriteTimeout and IdleTimeout
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		h.log.Error("Hijacking not supported")
		http.Error(w, "Hijacking not supported!", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		h.log.Error("Hijack failed", "error", err)
		http.Error(w, "Hijack failed!", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	conn.SetDeadline(time.Time{}) // infinite deadline

	// Enable TCP keep-alive for idle connections
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
		tcpConn.SetNoDelay(true)
	}

	chatIDs, err := h.tg.ResolveChatIDs(cleanChatIDs)
	if err != nil {
		h.log.Error("Resolve Chat ids failed", "error", err)
		http.Error(w, "Resolve chat ids failed!", http.StatusInternalServerError)
		return
	}

	outChan := h.router.Register(chatIDs)
	defer h.router.Unregister(outChan)

	for {
		select {
		case <-r.Context().Done():
			h.log.Info("Subscribe handler stopped", "reason", "context done")
			return
		case msg := <-outChan:
			data, err := json.Marshal(msg)
			if err != nil {
				h.log.Error("Failed to marshal message for SSE", "error", err)
				continue
			}

			sseData := fmt.Sprintf("data: %s\n\n", string(data))
			_, writeErr := fmt.Fprintf(bufrw, "%x\r\n%s\r\n", len(sseData), sseData)
			if writeErr != nil {
				h.log.Error("Failed to write SSE event", "error", writeErr)
				return
			}

			if flushErr := bufrw.Flush(); flushErr != nil {
				h.log.Error("Failed to flush SSE event", "error", flushErr)
				return
			}
		}
	}
}

func (h *Handlers) GetAttachment(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	url := vars["url"]
	if url == "" {
		h.log.Warn("Missing url parameter")
		respondError(w, http.StatusBadRequest, "url is required")
		return
	}

	h.log.Info("Get attachment request", "url", url)

	data, contentType, err := h.tg.GetAttachment(url)
	if err != nil {
		h.log.Error("Failed to get attachment", "url", url, "error", err)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.log.Info("Get attachment successful", "url", url, "content_type", contentType)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, types.ErrorResponse{Error: message})
}
