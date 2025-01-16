package api

import (
	"github.com/TicketsBot/GoPanel/utils"
	"github.com/TicketsBot/GoPanel/utils/types"
	"github.com/TicketsBot/database"
	v2 "github.com/TicketsBot/logarchiver/pkg/model/v2"
	"github.com/TicketsBot/worker/bot/customisation"
)

type (
	AutoCloseData struct {
		Enabled                 bool  `json:"enabled"`
		SinceOpenWithNoResponse int64 `json:"since_open_with_no_response"`
		SinceLastMessage        int64 `json:"since_last_message"`
		OnUserLeave             bool  `json:"on_user_leave"`
	}

	Ticket struct {
		TicketId    int           `json:"ticket_id"`
		CloseReason *string       `json:"close_reason"`
		ClosedBy    *uint64       `json:"closed_by"`
		Rating      *uint8        `json:"rating"`
		Transcript  v2.Transcript `json:"transcript"`
	}

	ColourMap map[customisation.Colour]utils.HexColour

	Settings struct {
		database.Settings
		ClaimSettings     database.ClaimSettings     `json:"claim_settings"`
		AutoCloseSettings AutoCloseData              `json:"auto_close"`
		TicketPermissions database.TicketPermissions `json:"ticket_permissions"`
		Colours           ColourMap                  `json:"colours"`

		WelcomeMessage    string                `json:"welcome_message"`
		TicketLimit       uint8                 `json:"ticket_limit"`
		Category          uint64                `json:"category,string"`
		ArchiveChannel    *uint64               `json:"archive_channel,string"`
		NamingScheme      database.NamingScheme `json:"naming_scheme"`
		UsersCanClose     bool                  `json:"users_can_close"`
		CloseConfirmation bool                  `json:"close_confirmation"`
		FeedbackEnabled   bool                  `json:"feedback_enabled"`
		Language          *string               `json:"language"`
	}
	Panel struct {
		database.Panel
		WelcomeMessage               *types.CustomEmbed                `json:"welcome_message"`
		UseCustomEmoji               bool                              `json:"use_custom_emoji"`
		Emoji                        types.Emoji                       `json:"emote"`
		Mentions                     []string                          `json:"mentions"`
		Teams                        []int                             `json:"teams"`
		UseServerDefaultNamingScheme bool                              `json:"use_server_default_naming_scheme"`
		AccessControlList            []database.PanelAccessControlRule `json:"access_control_list"`
	}
	MultiPanel struct {
		database.MultiPanel
		Panels []int `json:"panels"`
	}
	Tag struct {
		Id              string             `json:"id" validate:"required,min=1,max=16"`
		Trigger         string             `json:"trigger" validate:"required,min=1,max=32"`
		UseGuildCommand bool               `json:"use_guild_command"`
		Content         *string            `json:"content" validate:"omitempty,min=1,max=4096"`
		UseEmbed        bool               `json:"use_embed"`
		Embed           *types.CustomEmbed `json:"embed" validate:"omitempty,dive"`
	}
	Blacklist struct {
		Users types.UInt64StringSlice `json:"users"`
		Roles types.UInt64StringSlice `json:"roles"`
	}
	Form struct {
		database.Form
		Inputs []database.FormInput `json:"inputs"`
	}
	SupportTeam struct {
		Id         int                     `json:"id"`
		Name       string                  `json:"name"`
		OnCallRole *uint64                 `json:"on_call_role_id,string"`
		Users      types.UInt64StringSlice `json:"users"`
		Roles      types.UInt64StringSlice `json:"roles"`
	}
	Export struct {
		GuildId     uint64        `json:"guild_id,string"`
		Settings    Settings      `json:"settings"`
		Panels      []Panel       `json:"panels"`
		MultiPanels []MultiPanel  `json:"multi_panels"`
		Tickets     []Ticket      `json:"tickets"`
		Tags        []Tag         `json:"tags"`
		Blacklist   Blacklist     `json:"blacklist"`
		Forms       []Form        `json:"forms"`
		StaffTeams  []SupportTeam `json:"staff_teams"`
	}
)
