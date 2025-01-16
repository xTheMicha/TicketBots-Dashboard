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

	var (
		tags      []Tag
		blacklist *Blacklist
	)

	itemGroup, _ := errgroup.WithContext(context.Background())

	itemGroup.Go(func() (err error) {
		tags, err = getTags(ctx, guildId)
		return
	})

	itemGroup.Go(func() (err error) {
		blacklist, err = getBlacklist(ctx, guildId)
		return
	})

	if err := itemGroup.Wait(); err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	forms, err := getForms(ctx, guildId)
	if err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	staffTeams, err := getStaffTeams(ctx, guildId)
	if err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	ctx.JSON(200, Export{
		GuildId:     guildId,
		Settings:    settings,
		Panels:      panels,
		MultiPanels: multiPanelData,
		Tickets:     tickets,
		Tags:        tags,
		Blacklist:   *blacklist,
		Forms:       forms,
		StaffTeams:  staffTeams,
	})
}

func getStaffTeams(ctx *gin.Context, guildId uint64) ([]SupportTeam, error) {
	dbTeams, err := dbclient.Client.SupportTeam.Get(ctx, guildId)
	if err != nil {
		return nil, err
	}

	// prevent serving null
	if dbTeams == nil {
		dbTeams = make([]database.SupportTeam, 0)
	}

	var teams []SupportTeam

	for i := range dbTeams {
		group, _ := errgroup.WithContext(context.Background())
		i := i
		team := dbTeams[i]
		var (
			users []uint64
			roles []uint64
		)

		group.Go(func() error {
			users, err = dbclient.Client.SupportTeamMembers.Get(ctx, team.Id)
			return err
		})

		group.Go(func() error {
			roles, err = dbclient.Client.SupportTeamRoles.Get(ctx, team.Id)
			return err
		})

		if err := group.Wait(); err != nil {
			return nil, err
		}

		teams = append(teams, SupportTeam{
			Id:         team.Id,
			Name:       team.Name,
			OnCallRole: team.OnCallRole,
			Users:      users,
			Roles:      roles,
		})
	}

	return teams, nil
}

func getForms(ctx *gin.Context, guildId uint64) ([]Form, error) {
	dbForms, err := dbclient.Client.Forms.GetForms(ctx, guildId)
	if err != nil {
		return nil, err
	}

	dbFormInputs, err := dbclient.Client.FormInput.GetInputsForGuild(ctx, guildId)
	if err != nil {
		return nil, err
	}

	forms := make([]Form, len(dbForms))
	for i, form := range dbForms {
		formInputs, ok := dbFormInputs[form.Id]
		if !ok {
			formInputs = make([]database.FormInput, 0)
		}

		forms[i] = Form{
			Form:   form,
			Inputs: formInputs,
		}
	}
	return forms, nil
}

func getBlacklist(ctx *gin.Context, guildId uint64) (*Blacklist, error) {
	userBlacklist, err := dbclient.Client.Blacklist.GetBlacklistedUsers(ctx, guildId, 100000, 0)
	if err != nil {
		return nil, err
	}

	roleBlacklist, err := dbclient.Client.RoleBlacklist.GetBlacklistedRoles(ctx, guildId)
	if err != nil {
		return nil, err
	}
	return &Blacklist{
		Users: userBlacklist,
		Roles: roleBlacklist,
	}, nil
}

func getTags(ctx *gin.Context, guildId uint64) ([]Tag, error) {
	dbTags, err := dbclient.Client.Tag.GetByGuild(ctx, guildId)
	fmt.Println(len(dbTags))
	if err != nil {
		return nil, err
	}

	tags := make([]Tag, len(dbTags))

	i := 0
	for _, tag := range dbTags {
		var embed *types.CustomEmbed
		if tag.Embed != nil {
			embed = types.NewCustomEmbed(tag.Embed.CustomEmbed, tag.Embed.Fields)
		}

		tags[i] = Tag{
			Id:              tag.Id,
			UseGuildCommand: tag.ApplicationCommandId != nil,
			Content:         tag.Content,
			UseEmbed:        tag.Embed != nil,
			Embed:           embed,
		}

		i++
	}
	return tags, nil
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
