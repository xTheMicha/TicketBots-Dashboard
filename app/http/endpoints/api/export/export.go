package api

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/TicketsBot/GoPanel/botcontext"
	dbclient "github.com/TicketsBot/GoPanel/database"
	"github.com/TicketsBot/GoPanel/utils"
	"github.com/TicketsBot/GoPanel/utils/types"
	"github.com/TicketsBot/database"
	v2 "github.com/TicketsBot/logarchiver/pkg/model/v2"
	"github.com/TicketsBot/worker/bot/customisation"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
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
	Export struct {
		GuildId     uint64       `json:"guild_id"`
		Settings    Settings     `json:"settings"`
		Panels      []Panel      `json:"panels"`
		MultiPanels []MultiPanel `json:"multi_panels"`
		Tickets     []Ticket     `json:"tickets"`
	}
)

var activeColours = []customisation.Colour{customisation.Green, customisation.Red}

func ExportHandler(ctx *gin.Context) {
	guildId, selfId := ctx.Keys["guildid"].(uint64), ctx.Keys["userid"].(uint64)

	botCtx, err := botcontext.ContextForGuild(guildId)
	if err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	guild, err := botCtx.GetGuild(context.Background(), guildId)
	if err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	if guild.OwnerId != selfId {
		ctx.JSON(403, utils.ErrorStr("Only the server owner export server data"))
		return
	}

	settings := getSettings(ctx, guildId)

	multiPanels, err := dbclient.Client.MultiPanels.GetByGuild(ctx, guildId)
	if err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	multiPanelData := make([]MultiPanel, len(multiPanels))

	group, _ := errgroup.WithContext(context.Background())

	var dbTickets []database.Ticket

	// ticket list
	group.Go(func() (err error) {
		dbTickets, err = dbclient.Client.Tickets.GetByOptions(ctx, database.TicketQueryOptions{
			GuildId: guildId,
		})

		return
	})

	for i := range multiPanels {
		i := i
		multiPanel := multiPanels[i]

		multiPanelData[i] = MultiPanel{
			MultiPanel: multiPanel,
		}

		group.Go(func() error {
			panels, err := dbclient.Client.MultiPanelTargets.GetPanels(ctx, multiPanel.Id)
			if err != nil {
				return err
			}

			panelIds := make([]int, len(panels))
			for i, panel := range panels {
				panelIds[i] = panel.PanelId
			}

			multiPanelData[i].Panels = panelIds

			return nil
		})
	}

	var dbPanels []database.PanelWithWelcomeMessage
	var panelACLs map[int][]database.PanelAccessControlRule
	var panelFields map[int][]database.EmbedField
	// panels
	group.Go(func() (err error) {
		dbPanels, err = dbclient.Client.Panel.GetByGuildWithWelcomeMessage(ctx, guildId)
		return
	})

	// access control lists
	group.Go(func() (err error) {
		panelACLs, err = dbclient.Client.PanelAccessControlRules.GetAllForGuild(ctx, guildId)
		return
	})

	// all fields
	group.Go(func() (err error) {
		panelFields, err = dbclient.Client.EmbedFields.GetAllFieldsForPanels(ctx, guildId)
		return
	})

	if err := group.Wait(); err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	// ratings
	ticketIds := make([]int, len(dbTickets))
	for i, ticket := range dbTickets {
		ticketIds[i] = ticket.Id
	}

	ticketGroup, _ := errgroup.WithContext(context.Background())

	var (
		ratings      map[int]uint8
		closeReasons map[int]database.CloseMetadata
		tickets      = make([]Ticket, len(dbTickets))
	)

	ticketGroup.Go(func() (err error) {
		ratings, err = dbclient.Client.ServiceRatings.GetMulti(ctx, guildId, ticketIds)
		return
	})

	ticketGroup.Go(func() (err error) {
		closeReasons, err = dbclient.Client.CloseReason.GetMulti(ctx, guildId, ticketIds)
		return
	})

	if err := ticketGroup.Wait(); err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	transcriptGroup, _ := errgroup.WithContext(context.Background())

	for i := range dbTickets {
		ticket := dbTickets[i]

		transcriptGroup.Go(func() (err error) {
			var transcriptMessages v2.Transcript
			transcriptMessages, err = utils.ArchiverClient.Get(ctx, guildId, ticket.Id)
			if err != nil {
				if err.Error() == "Transcript not found" {
					err = nil
				}
				return
			}

			transcript := Ticket{
				TicketId:   ticket.Id,
				Transcript: transcriptMessages,
			}

			if v, ok := ratings[ticket.Id]; ok {
				transcript.Rating = &v
			}

			if v, ok := closeReasons[ticket.Id]; ok {
				transcript.CloseReason = v.Reason
				transcript.ClosedBy = v.ClosedBy
			}

			tickets[i] = transcript

			return
		})

	}

	if err := transcriptGroup.Wait(); err != nil {
		fmt.Println(err)
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	panels := make([]Panel, len(dbPanels))

	panelGroup, _ := errgroup.WithContext(context.Background())

	for i, p := range dbPanels {
		i := i
		p := p

		panelGroup.Go(func() error {
			var mentions []string

			// get if we should mention the ticket opener
			shouldMention, err := dbclient.Client.PanelUserMention.ShouldMentionUser(ctx, p.PanelId)
			if err != nil {
				return err
			}

			if shouldMention {
				mentions = append(mentions, "user")
			}

			// get role mentions
			roles, err := dbclient.Client.PanelRoleMentions.GetRoles(ctx, p.PanelId)
			if err != nil {
				return err
			}

			// convert to strings
			for _, roleId := range roles {
				mentions = append(mentions, strconv.FormatUint(roleId, 10))
			}

			teams, err := dbclient.Client.PanelTeams.GetTeamIds(ctx, p.PanelId)
			if err != nil {
				return err
			}

			if teams == nil {
				teams = make([]int, 0)
			}

			var welcomeMessage *types.CustomEmbed
			if p.WelcomeMessage != nil {
				fields := panelFields[p.WelcomeMessage.Id]
				welcomeMessage = types.NewCustomEmbed(p.WelcomeMessage, fields)
			}

			acl := panelACLs[p.PanelId]
			if acl == nil {
				acl = make([]database.PanelAccessControlRule, 0)
			}

			panels[i] = Panel{
				Panel:                        p.Panel,
				WelcomeMessage:               welcomeMessage,
				UseCustomEmoji:               p.EmojiId != nil,
				Emoji:                        types.NewEmoji(p.EmojiName, p.EmojiId),
				Mentions:                     mentions,
				Teams:                        teams,
				UseServerDefaultNamingScheme: p.NamingScheme == nil,
				AccessControlList:            acl,
			}

			return nil

		})
	}

	if err := panelGroup.Wait(); err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	ctx.JSON(200, Export{
		GuildId:     guildId,
		Settings:    settings,
		Panels:      panels,
		MultiPanels: multiPanelData,
		Tickets:     tickets,
	})
}

func getSettings(ctx *gin.Context, guildId uint64) Settings {
	var settings Settings
	group, _ := errgroup.WithContext(context.Background())

	// main settings
	group.Go(func() (err error) {
		settings.Settings, err = dbclient.Client.Settings.Get(ctx, guildId)
		return
	})

	// claim settings
	group.Go(func() (err error) {
		settings.ClaimSettings, err = dbclient.Client.ClaimSettings.Get(ctx, guildId)
		return
	})

	// auto close settings
	group.Go(func() error {
		tmp, err := dbclient.Client.AutoClose.Get(ctx, guildId)
		if err != nil {
			return err
		}

		settings.AutoCloseSettings = convertToAutoCloseData(tmp)
		return nil
	})

	// ticket permissions
	group.Go(func() (err error) {
		settings.TicketPermissions, err = dbclient.Client.TicketPermissions.Get(ctx, guildId)
		return
	})

	// colour map
	group.Go(func() (err error) {
		settings.Colours, err = getColourMap(guildId)
		return
	})

	// welcome message
	group.Go(func() (err error) {
		settings.WelcomeMessage, err = dbclient.Client.WelcomeMessages.Get(ctx, guildId)
		if err == nil && settings.WelcomeMessage == "" {
			settings.WelcomeMessage = "Thank you for contacting support.\nPlease describe your issue and await a response."
		}

		return
	})

	// ticket limit
	group.Go(func() (err error) {
		settings.TicketLimit, err = dbclient.Client.TicketLimit.Get(ctx, guildId)
		if err == nil && settings.TicketLimit == 0 {
			settings.TicketLimit = 5 // Set default
		}

		return
	})

	// category
	group.Go(func() (err error) {
		settings.Category, err = dbclient.Client.ChannelCategory.Get(ctx, guildId)
		return
	})

	// archive channel
	group.Go(func() (err error) {
		settings.ArchiveChannel, err = dbclient.Client.ArchiveChannel.Get(ctx, guildId)
		return
	})

	// allow users to close
	group.Go(func() (err error) {
		settings.UsersCanClose, err = dbclient.Client.UsersCanClose.Get(ctx, guildId)
		return
	})

	// naming scheme
	group.Go(func() (err error) {
		settings.NamingScheme, err = dbclient.Client.NamingScheme.Get(ctx, guildId)
		return
	})

	// close confirmation
	group.Go(func() (err error) {
		settings.CloseConfirmation, err = dbclient.Client.CloseConfirmation.Get(ctx, guildId)
		return
	})

	// close confirmation
	group.Go(func() (err error) {
		settings.FeedbackEnabled, err = dbclient.Client.FeedbackEnabled.Get(ctx, guildId)
		return
	})

	// language
	group.Go(func() error {
		locale, err := dbclient.Client.ActiveLanguage.Get(ctx, guildId)
		if err != nil {
			return err
		}

		if locale != "" {
			settings.Language = utils.Ptr(locale)
		}

		return nil
	})

	if err := group.Wait(); err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
	}

	return settings
}

func getColourMap(guildId uint64) (ColourMap, error) {
	raw, err := dbclient.Client.CustomColours.GetAll(context.Background(), guildId)
	if err != nil {
		return nil, err
	}

	colours := make(ColourMap)
	for id, hex := range raw {
		if !utils.Exists(activeColours, customisation.Colour(id)) {
			continue
		}

		colours[customisation.Colour(id)] = utils.HexColour(hex)
	}

	for _, id := range activeColours {
		if _, ok := colours[id]; !ok {
			colours[id] = utils.HexColour(customisation.DefaultColours[id])
		}
	}

	return colours, nil
}

func convertToAutoCloseData(settings database.AutoCloseSettings) (body AutoCloseData) {
	body.Enabled = settings.Enabled

	if settings.SinceOpenWithNoResponse != nil {
		body.SinceOpenWithNoResponse = int64(*settings.SinceOpenWithNoResponse / time.Second)
	}

	if settings.SinceLastMessage != nil {
		body.SinceLastMessage = int64(*settings.SinceLastMessage / time.Second)
	}

	if settings.OnUserLeave != nil {
		body.OnUserLeave = *settings.OnUserLeave
	}

	return
}
