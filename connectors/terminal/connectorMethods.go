package terminal

import (
	"fmt"

	"github.com/lnxjedi/gopherbot/bot"
)

func (tc *termConnector) MessageHeard(u, c string) {
	return
}

// GetUserAttribute returns a string attribute or nil if slack doesn't
// have that information
func (tc *termConnector) GetProtocolUserAttribute(u, attr string) (value string, ret bot.RetVal) {
	i, exists := userMap[u]
	if !exists {
		return "", bot.UserNotFound
	}
	user := tc.users[i]
	switch attr {
	case "email":
		return user.Email, bot.Ok
	case "internalid":
		return user.InternalID, bot.Ok
	case "realname", "fullname", "real name", "full name":
		return user.FullName, bot.Ok
	case "firstname", "first name":
		return user.FirstName, bot.Ok
	case "lastname", "last name":
		return user.LastName, bot.Ok
	case "phone":
		return user.Phone, bot.Ok
	// that's all the attributes we can currently get from slack
	default:
		return "", bot.AttributeNotFound
	}
}

// SendProtocolChannelMessage sends a message to a channel
func (tc *termConnector) SendProtocolChannelMessage(ch string, msg string, f bot.MessageFormat) (ret bot.RetVal) {
	return tc.sendMessage(ch, msg, f)
}

// SendProtocolChannelMessage sends a message to a channel
func (tc *termConnector) SendProtocolUserChannelMessage(u, ch, msg string, f bot.MessageFormat) (ret bot.RetVal) {
	msg = "@" + u + " " + msg
	return tc.sendMessage(ch, msg, f)
}

// SendProtocolUserMessage sends a direct message to a user
func (tc *termConnector) SendProtocolUserMessage(u string, msg string, f bot.MessageFormat) (ret bot.RetVal) {
	return tc.sendMessage(fmt.Sprintf("(dm:%s)", u), msg, f)
}

// JoinChannel joins a channel given it's human-readable name, e.g. "general"
// Only useful for connectors that require it, a noop otherwise
func (tc *termConnector) JoinChannel(c string) (ret bot.RetVal) {
	return bot.Ok
}
