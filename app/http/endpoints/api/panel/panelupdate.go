package api

import (
	"context"
	"errors"
	"github.com/TicketsBot/GoPanel/botcontext"
	dbclient "github.com/TicketsBot/GoPanel/database"
	"github.com/TicketsBot/GoPanel/rpc"
	"github.com/TicketsBot/GoPanel/utils"
	"github.com/TicketsBot/common/premium"
	"github.com/TicketsBot/database"
	"github.com/gin-gonic/gin"
	"github.com/rxdn/gdl/rest"
	"github.com/rxdn/gdl/rest/request"
	"golang.org/x/sync/errgroup"
	"strconv"
	"sync"
)

func UpdatePanel(ctx *gin.Context) {
	guildId := ctx.Keys["guildid"].(uint64)

	botContext, err := botcontext.ContextForGuild(guildId)
	if err != nil {
		ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
		return
	}

	var data panelBody

	if err := ctx.BindJSON(&data); err != nil {
		ctx.AbortWithStatusJSON(400, utils.ErrorJson(err))
		return
	}

	panelId, err := strconv.Atoi(ctx.Param("panelid"))
	if err != nil {
		ctx.AbortWithStatusJSON(400, utils.ErrorJson(err))
		return
	}

	// get existing
	existing, err := dbclient.Client.Panel.GetById(panelId)
	if err != nil {
		ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
		return
	}

	// check guild ID matches
	if existing.GuildId != guildId {
		ctx.AbortWithStatusJSON(400, gin.H{
			"success": false,
			"error":   "Guild ID does not match",
		})
		return
	}

	if !data.doValidations(ctx, guildId) {
		return
	}

	// check if this will break a multi-panel;
	// first, get any multipanels this panel belongs to
	multiPanels, err := dbclient.Client.MultiPanelTargets.GetMultiPanels(existing.PanelId)
	if err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	var wouldHaveDuplicateEmote bool

	{
		var duplicateLock sync.Mutex

		group, _ := errgroup.WithContext(context.Background())
		for _, multiPanelId := range multiPanels {
			multiPanelId := multiPanelId

			group.Go(func() error {
				// get the sub-panels of the multi-panel
				subPanels, err := dbclient.Client.MultiPanelTargets.GetPanels(multiPanelId)
				if err != nil {
					return err
				}

				for _, subPanel := range subPanels {
					if subPanel.MessageId == existing.MessageId {
						continue
					}

					if subPanel.ReactionEmote == data.Emote {
						duplicateLock.Lock()
						wouldHaveDuplicateEmote = true
						duplicateLock.Unlock()
						break
					}
				}

				return nil
			})
		}

		if err := group.Wait(); err != nil {
			ctx.JSON(500, utils.ErrorJson(err))
			return
		}
	}

	if wouldHaveDuplicateEmote {
		ctx.JSON(400, utils.ErrorJson(errors.New("Changing the reaction emote to this value would cause a conflict in a multi-panel")))
		return
	}

	// check if we need to update the message
	shouldUpdateMessage := uint32(existing.Colour) != data.Colour ||
		existing.ChannelId != data.ChannelId ||
		existing.Content != data.Content ||
		existing.Title != data.Title ||
		existing.ReactionEmote != data.Emote

	emoji, _ := data.getEmoji() // already validated
	newMessageId := existing.MessageId

	if shouldUpdateMessage {
		// delete old message, ignoring error
		_ = rest.DeleteMessage(botContext.Token, botContext.RateLimiter, existing.ChannelId, existing.MessageId)

		premiumTier := rpc.PremiumClient.GetTierByGuildId(guildId, true, botContext.Token, botContext.RateLimiter)
		newMessageId, err = data.sendEmbed(&botContext, existing.Title, existing.CustomId, existing.ReactionEmote, premiumTier > premium.None)
		if err != nil {
			var unwrapped request.RestError
			if errors.As(err, &unwrapped) && unwrapped.StatusCode == 403 {
				ctx.AbortWithStatusJSON(500, gin.H{
					"success": false,
					"error":   "I do not have permission to send messages in the specified channel",
				})
			} else {
				// TODO: Most appropriate error?
				ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
			}

			return
		}
	}

	// Store in DB
	panel := database.Panel{
		PanelId:         panelId,
		MessageId:       newMessageId,
		ChannelId:       data.ChannelId,
		GuildId:         guildId,
		Title:           data.Title,
		Content:         data.Content,
		Colour:          int32(data.Colour),
		TargetCategory:  data.CategoryId,
		ReactionEmote:   emoji,
		WelcomeMessage:  data.WelcomeMessage,
		WithDefaultTeam: utils.ContainsString(data.Teams, "default"),
		CustomId:        existing.CustomId,
	}

	if err = dbclient.Client.Panel.Update(panel); err != nil {
		ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
		return
	}

	// insert role mention data
	// delete old data
	if err = dbclient.Client.PanelRoleMentions.DeleteAll(panel.PanelId); err != nil {
		ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
		return
	}

	// string is role ID or "user" to mention the ticket opener
	var shouldMentionUser bool
	for _, mention := range data.Mentions {
		if mention == "user" {
			shouldMentionUser = true
		} else {
			roleId, err := strconv.ParseUint(mention, 10, 64)
			if err != nil {
				ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
				return
			}

			// should we check the role is a valid role in the guild?
			// not too much of an issue if it isnt
			if err = dbclient.Client.PanelRoleMentions.Add(panel.PanelId, roleId); err != nil {
				ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
				return
			}
		}
	}

	if err = dbclient.Client.PanelUserMention.Set(panel.PanelId, shouldMentionUser); err != nil {
		ctx.AbortWithStatusJSON(500, utils.ErrorJson(err))
		return
	}

	// insert support teams
	// TODO: Stop race conditions - 1 transaction
	// delete teams
	if err := dbclient.Client.PanelTeams.DeleteAll(panel.PanelId); err != nil {
		ctx.JSON(500, utils.ErrorJson(err))
		return
	}

	// insert new
	if responseCode, err := insertTeams(guildId, panel.PanelId, data.Teams); err != nil {
		ctx.JSON(responseCode, utils.ErrorJson(err))
		return
	}

	ctx.JSON(200, gin.H{
		"success": true,
	})
}
