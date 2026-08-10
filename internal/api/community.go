package api

import "net/http"

// 社群渠道入口（GET /api/community/channels）。
//
// **公开端点·不要求登录** —— landing / footer 也要拉。
//
// 空 = 前端展示"敬请期待"占位（不渲染死链）。

type communityChannelDTO struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	NameI18n map[string]string `json:"name_i18n,omitempty"`
	URL      string            `json:"url"`
}

type communityChannelsResp struct {
	Channels []communityChannelDTO `json:"channels"`
}

// handleListCommunityChannels · GET /api/community/channels · 公开。
func (s *Server) handleListCommunityChannels(w http.ResponseWriter, r *http.Request) error {
	items := make([]communityChannelDTO, 0, len(s.communityChannels))
	for _, c := range s.communityChannels {
		if !c.Enabled || c.URL == "" {
			continue
		}
		items = append(items, communityChannelDTO{
			ID:       c.ID,
			Name:     c.Name,
			NameI18n: c.NameI18n,
			URL:      c.URL,
		})
	}
	writeJSON(w, http.StatusOK, communityChannelsResp{Channels: items})
	return nil
}
