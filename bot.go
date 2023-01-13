package main

import (
	"context"
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"golang.org/x/sync/errgroup"
	"log"
	"strconv"
	"sync"
)

type PollRole struct {
	emoji string
	label string
	limit int64
	def   int64
}

type bot struct {
	mx    *sync.RWMutex
	state *state.State
	roles map[discord.RoleID]PollRole
	users map[discord.UserID]discord.RoleID
}

var commands = []api.CreateCommandData{
	{
		Name:        "poll",
		Description: "Создать выбор ролей. Сбрасывает текущий прогресс выбора.",
	},
	{
		Name:        "clear",
		Description: "Удаляет все роли.",
	},
	{
		Name:        "status",
		Description: "Посмотреть все роли.",
	},
	{
		Name:        "add",
		Description: "Добавить роль (кнопку).",
		Options: []discord.CommandOption{
			&discord.StringOption{
				OptionName:  "label",
				Description: "Текст кнопки",
				Required:    true,
			},
			&discord.RoleOption{
				OptionName:  "role",
				Description: "Роль назначаемая по кнопке",
				Required:    true,
			},
			&discord.IntegerOption{
				OptionName:  "limit",
				Description: "Количество мест в роли",
				Required:    true,
				Min:         option.NewInt(1),
			},
			&discord.StringOption{
				OptionName:  "emoji",
				Description: "Emoji на кнопке",
				Required:    true,
			},
		},
	},
}

func (b *bot) HandleInteractionCreateEvent(e *gateway.InteractionCreateEvent) {
	var commonResp api.InteractionResponse
	switch data := e.Data.(type) {
	case *discord.CommandInteraction:
		commonResp = b.handleCommandInteraction(e.GuildID, data)
	case discord.ComponentInteraction:
		role, err := discord.ParseSnowflake(string(data.ID()))
		if err != nil {
			log.Println("failed bad role", err)
			return
		}
		userID := e.Member.User.ID
		roleID := discord.RoleID(role)
		wasRole := discord.NullRoleID
		finalRole := discord.NullRoleID

		b.mx.Lock()
		if v, ok := b.roles[roleID]; ok {
			if wasRole, ok = b.users[userID]; ok && wasRole != roleID {
				vv := b.roles[wasRole]
				if v.limit-1 >= 0 {
					vv.limit++
					b.roles[wasRole] = vv
				}
			}
			v.limit--
			if v.limit >= 0 && wasRole != roleID {
				b.roles[roleID] = v
				b.users[userID] = roleID
				finalRole = roleID
			}
		}
		b.mx.Unlock()

		if finalRole == roleID {
			if wasRole != discord.NullRoleID {
				_ = b.state.RemoveRole(e.GuildID, userID, wasRole, "Change role by poll")
			}
			err := b.state.AddRole(e.GuildID, userID, roleID, api.AddRoleData{AuditLogReason: "Add role by poll"})
			if err != nil {
				log.Println("failed to add role", err)
			}

		}
		commonResp = b.buildButtonsResp(api.UpdateMessage, false)
	default:
		return
	}

	if err := b.state.RespondInteraction(e.ID, e.Token, commonResp); err != nil {
		log.Println("failed to send interaction callback:", err)
	}
}

func (b *bot) handleCommandInteraction(guildID discord.GuildID, data *discord.CommandInteraction) api.InteractionResponse {
	switch data.Name {
	case "poll":
		eg, _ := errgroup.WithContext(context.Background())
		b.mx.Lock()
		for k, v := range b.roles {
			v.limit = v.def
			b.roles[k] = v
		}
		for k, v := range b.users {
			eg.Go(func() error {
				return b.state.RemoveRole(guildID, k, v, "Reset poll")
			})
		}
		b.mx.Unlock()

		if err := eg.Wait(); err != nil {
			return NullableStringResp("ERROR: internal")
		}
		return b.buildButtonsResp(api.MessageInteractionWithSource, false)
	case "clear":
		b.roles = make(map[discord.RoleID]PollRole, 5)
	case "status":
		return b.buildButtonsResp(api.MessageInteractionWithSource, true)
	case "add":
		label := data.Options.Find("label").String()
		role := data.Options.Find("role").String()
		roleID, err := discord.ParseSnowflake(role)
		if err != nil {
			return NullableStringResp("ERROR: bad role")
		}
		limit, err := data.Options.Find("limit").IntValue()
		if err != nil {
			return NullableStringResp("ERROR: bad limit")
		}

		emoji := data.Options.Find("emoji").String()
		b.mx.Lock()
		b.roles[discord.RoleID(roleID)] = PollRole{
			emoji: emoji,
			label: label,
			limit: limit,
			def:   limit,
		}
		b.mx.Unlock()

	default:
		return NullableStringResp("ERROR: unknown command: " + data.Name)
	}
	return NullableStringResp("Success")
}

func (b *bot) buildButtonsResp(typ api.InteractionResponseType, disableAll bool) api.InteractionResponse {
	var buttons []discord.InteractiveComponent
	b.mx.RLock()
	for k, v := range b.roles {
		buttons = append(buttons, &discord.ButtonComponent{
			CustomID: discord.ComponentID(k.String()),
			Label:    v.label + " " + strconv.FormatInt(v.limit, 10),
			Emoji:    &discord.ComponentEmoji{Name: v.emoji},
			Style:    discord.SecondaryButtonStyle(),
			Disabled: disableAll || v.limit == 0,
		})
	}
	b.mx.RUnlock()

	if len(buttons) == 0 {
		return NullableStringResp("Нет ролей")
	}

	row := discord.ActionRowComponent(buttons)
	resp := api.InteractionResponse{
		Type: typ,
		Data: &api.InteractionResponseData{
			Content:    option.NewNullableString("Выбери свою роль!"),
			Components: discord.ComponentsPtr(&row),
		},
	}

	return resp
}

func NullableStringResp(msg string) api.InteractionResponse {
	return api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString(msg),
		},
	}
}
