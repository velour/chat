package irc

// Parsing of IRC messages as specified in RFC 1459.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// MaxBytes is the maximum length of a message in bytes.
const MaxBytes = 512

// TooLongError indicates that a received message was too long.
type TooLongError struct {
	// Message is the truncated message text.
	Message []byte
	// NTrunc is the number of truncated bytes.
	NTrunc int
}

func (m TooLongError) Error() string {
	return fmt.Sprintf("Message is too long (%d bytes truncated): %s",
		m.NTrunc, m.Message)
}

// A Message is the basic unit of communication
// in the IRC protocol.
type Message struct {
	// Origin is either the nick or server that
	// originated the message.
	Origin string

	// User is the user name of the user that
	// originated the message.
	//
	// This field is typically set in server to
	// client communication when the
	// message originated from a client.
	User string

	// Host is the host name of the user that
	// originated the message.
	//
	// This field is typically set in server to
	// client communication when the
	// message originated from a client.
	Host string

	// Command is the command.
	Command string

	// Arguments is the argument list.
	Arguments []string
}

// Bytes returns the byte representation of a message.
// The returned message may be longer than MaxMessageLength bytes.
func (m Message) Bytes() []byte {
	buf := bytes.NewBuffer(nil)
	if m.Origin != "" {
		buf.WriteRune(':')
		buf.WriteString(m.Origin)
		if m.User != "" {
			buf.WriteRune('!')
			buf.WriteString(m.User)
			buf.WriteRune('@')
			buf.WriteString(m.Host)
		}
		buf.WriteRune(' ')
	}
	buf.WriteString(m.Command)
	for i, a := range m.Arguments {
		if i == len(m.Arguments)-1 {
			buf.WriteString(" :")
		} else {
			buf.WriteRune(' ')
		}
		buf.WriteString(a)
	}
	buf.WriteString(eom)
	return buf.Bytes()
}

// eom is the end of message marker.
const eom = "\r\n"

// Read returns the next message.
func read(in io.ByteReader) (Message, error) {
	var msg []byte
	for {
		switch c, err := in.ReadByte(); {
		case err == io.EOF && len(msg) > 0:
			return Message{}, errors.New("unexpected end of file")

		case err != nil:
			return Message{}, err

		case c == '\000':
			return Message{}, errors.New("unexpected null")

			//		case c == '\n':
			//			// Technically an invalid message, but instead we just strip it.

		case c == '\r':
			switch c, err = in.ReadByte(); {
			case err == io.EOF:
				return Message{}, errors.New("unexpected end of file")
			case err != nil:
				return Message{}, err
			case c != '\n':
				return Message{}, errors.New("unexpected carrage return")
			case len(msg) == 0:
				continue
			default:
				return Parse(msg)
			}

		case len(msg) >= MaxBytes-len(eom):
			n, _ := junk(in)
			err := TooLongError{Message: msg[:len(msg)-1], NTrunc: n + 1}
			return Message{}, err

		default:
			msg = append(msg, c)
		}
	}
}

func junk(in io.ByteReader) (int, error) {
	var last byte
	n := 0
	for {
		c, err := in.ReadByte()
		if err != nil {
			return n, err
		}
		n++
		if last == eom[0] && c == eom[1] {
			break
		}
		last = c
	}
	return n - 1, nil
}

// Parse parses a message.
func Parse(data []byte) (Message, error) {
	if len(data) > MaxBytes {
		return Message{}, TooLongError{
			Message: data[:MaxBytes],
			NTrunc:  len(data) - MaxBytes,
		}
	}
	if len(data) == 0 {
		return Message{}, nil
	}

	var msg Message
	if data[0] == ':' {
		var prefix []byte
		prefix, data = split(data[1:], ' ')
		origin, prefix := split(prefix, '!')
		user, host := split(prefix, '@')
		msg.Origin = string(origin)
		msg.User = string(user)
		msg.Host = string(host)
	}

	cmd, data := split(data, ' ')
	msg.Command = string(cmd)

	for len(data) > 0 {
		var arg []byte
		if data[0] == ':' {
			arg, data = data[1:], nil
		} else {
			arg, data = split(data, ' ')
		}
		msg.Arguments = append(msg.Arguments, string(arg))
	}
	return msg, nil
}

func split(data []byte, delim byte) ([]byte, []byte) {
	fs := bytes.SplitN(data, []byte{delim}, 2)
	switch len(fs) {
	case 0:
		return nil, nil
	case 1:
		return fs[0], nil
	default:
		if delim == ' ' {
			fs[1] = bytes.TrimLeft(fs[1], " ")
		}
		return fs[0], fs[1]
	}
}

// Command names as listed in RFC 2812.
const (
	PASS                  = "PASS"
	NICK                  = "NICK"
	USER                  = "USER"
	OPER                  = "OPER"
	MODE                  = "MODE"
	SERVICE               = "SERVICE"
	QUIT                  = "QUIT"
	SQUIT                 = "SQUIT"
	JOIN                  = "JOIN"
	PART                  = "PART"
	TOPIC                 = "TOPIC"
	NAMES                 = "NAMES"
	LIST                  = "LIST"
	INVITE                = "INVITE"
	KICK                  = "KICK"
	PRIVMSG               = "PRIVMSG"
	NOTICE                = "NOTICE"
	MOTD                  = "MOTD"
	LUSERS                = "LUSERS"
	VERSION               = "VERSION"
	STATS                 = "STATS"
	LINKS                 = "LINKS"
	TIME                  = "TIME"
	CONNECT               = "CONNECT"
	TRACE                 = "TRACE"
	ADMIN                 = "ADMIN"
	INFO                  = "INFO"
	SERVLIST              = "SERVLIST"
	SQUERY                = "SQUERY"
	WHO                   = "WHO"
	WHOIS                 = "WHOIS"
	WHOWAS                = "WHOWAS"
	KILL                  = "KILL"
	PING                  = "PING"
	PONG                  = "PONG"
	ERROR                 = "ERROR"
	AWAY                  = "AWAY"
	REHASH                = "REHASH"
	DIE                   = "DIE"
	RESTART               = "RESTART"
	SUMMON                = "SUMMON"
	USERS                 = "USERS"
	WALLOPS               = "WALLOPS"
	USERHOST              = "USERHOST"
	ISON                  = "ISON"
	RPL_WELCOME           = "001"
	RPL_YOURHOST          = "002"
	RPL_CREATED           = "003"
	RPL_MYINFO            = "004"
	RPL_BOUNCE            = "005"
	RPL_USERHOST          = "302"
	RPL_ISON              = "303"
	RPL_AWAY              = "301"
	RPL_UNAWAY            = "305"
	RPL_NOWAWAY           = "306"
	RPL_WHOISUSER         = "311"
	RPL_WHOISSERVER       = "312"
	RPL_WHOISOPERATOR     = "313"
	RPL_WHOISIDLE         = "317"
	RPL_ENDOFWHOIS        = "318"
	RPL_WHOISCHANNELS     = "319"
	RPL_WHOWASUSER        = "314"
	RPL_ENDOFWHOWAS       = "369"
	RPL_LISTSTART         = "321"
	RPL_LIST              = "322"
	RPL_LISTEND           = "323"
	RPL_UNIQOPIS          = "325"
	RPL_CHANNELMODEIS     = "324"
	RPL_NOTOPIC           = "331"
	RPL_TOPIC             = "332"
	RPL_TOPICWHOTIME      = "333" // ircu specific (not in the RFC)
	RPL_INVITING          = "341"
	RPL_SUMMONING         = "342"
	RPL_INVITELIST        = "346"
	RPL_ENDOFINVITELIST   = "347"
	RPL_EXCEPTLIST        = "348"
	RPL_ENDOFEXCEPTLIST   = "349"
	RPL_VERSION           = "351"
	RPL_WHOREPLY          = "352"
	RPL_ENDOFWHO          = "315"
	RPL_NAMREPLY          = "353"
	RPL_ENDOFNAMES        = "366"
	RPL_LINKS             = "364"
	RPL_ENDOFLINKS        = "365"
	RPL_BANLIST           = "367"
	RPL_ENDOFBANLIST      = "368"
	RPL_INFO              = "371"
	RPL_ENDOFINFO         = "374"
	RPL_MOTDSTART         = "375"
	RPL_MOTD              = "372"
	RPL_ENDOFMOTD         = "376"
	RPL_YOUREOPER         = "381"
	RPL_REHASHING         = "382"
	RPL_YOURESERVICE      = "383"
	RPL_TIME              = "391"
	RPL_USERSSTART        = "392"
	RPL_USERS             = "393"
	RPL_ENDOFUSERS        = "394"
	RPL_NOUSERS           = "395"
	RPL_TRACELINK         = "200"
	RPL_TRACECONNECTING   = "201"
	RPL_TRACEHANDSHAKE    = "202"
	RPL_TRACEUNKNOWN      = "203"
	RPL_TRACEOPERATOR     = "204"
	RPL_TRACEUSER         = "205"
	RPL_TRACESERVER       = "206"
	RPL_TRACESERVICE      = "207"
	RPL_TRACENEWTYPE      = "208"
	RPL_TRACECLASS        = "209"
	RPL_TRACERECONNECT    = "210"
	RPL_TRACELOG          = "261"
	RPL_TRACEEND          = "262"
	RPL_STATSLINKINFO     = "211"
	RPL_STATSCOMMANDS     = "212"
	RPL_ENDOFSTATS        = "219"
	RPL_STATSUPTIME       = "242"
	RPL_STATSOLINE        = "243"
	RPL_UMODEIS           = "221"
	RPL_SERVLIST          = "234"
	RPL_SERVLISTEND       = "235"
	RPL_LUSERCLIENT       = "251"
	RPL_LUSEROP           = "252"
	RPL_LUSERUNKNOWN      = "253"
	RPL_LUSERCHANNELS     = "254"
	RPL_LUSERME           = "255"
	RPL_ADMINME           = "256"
	RPL_ADMINLOC1         = "257"
	RPL_ADMINLOC2         = "258"
	RPL_ADMINEMAIL        = "259"
	RPL_TRYAGAIN          = "263"
	ERR_NOSUCHNICK        = "401"
	ERR_NOSUCHSERVER      = "402"
	ERR_NOSUCHCHANNEL     = "403"
	ERR_CANNOTSENDTOCHAN  = "404"
	ERR_TOOMANYCHANNELS   = "405"
	ERR_WASNOSUCHNICK     = "406"
	ERR_TOOMANYTARGETS    = "407"
	ERR_NOSUCHSERVICE     = "408"
	ERR_NOORIGIN          = "409"
	ERR_NORECIPIENT       = "411"
	ERR_NOTEXTTOSEND      = "412"
	ERR_NOTOPLEVEL        = "413"
	ERR_WILDTOPLEVEL      = "414"
	ERR_BADMASK           = "415"
	ERR_UNKNOWNCOMMAND    = "421"
	ERR_NOMOTD            = "422"
	ERR_NOADMININFO       = "423"
	ERR_FILEERROR         = "424"
	ERR_NONICKNAMEGIVEN   = "431"
	ERR_ERRONEUSNICKNAME  = "432"
	ERR_NICKNAMEINUSE     = "433"
	ERR_NICKCOLLISION     = "436"
	ERR_UNAVAILRESOURCE   = "437"
	ERR_USERNOTINCHANNEL  = "441"
	ERR_NOTONCHANNEL      = "442"
	ERR_USERONCHANNEL     = "443"
	ERR_NOLOGIN           = "444"
	ERR_SUMMONDISABLED    = "445"
	ERR_USERSDISABLED     = "446"
	ERR_NOTREGISTERED     = "451"
	ERR_NEEDMOREPARAMS    = "461"
	ERR_ALREADYREGISTRED  = "462"
	ERR_NOPERMFORHOST     = "463"
	ERR_PASSWDMISMATCH    = "464"
	ERR_YOUREBANNEDCREEP  = "465"
	ERR_YOUWILLBEBANNED   = "466"
	ERR_KEYSET            = "467"
	ERR_CHANNELISFULL     = "471"
	ERR_UNKNOWNMODE       = "472"
	ERR_INVITEONLYCHAN    = "473"
	ERR_BANNEDFROMCHAN    = "474"
	ERR_BADCHANNELKEY     = "475"
	ERR_BADCHANMASK       = "476"
	ERR_NOCHANMODES       = "477"
	ERR_BANLISTFULL       = "478"
	ERR_NOPRIVILEGES      = "481"
	ERR_CHANOPRIVSNEEDED  = "482"
	ERR_CANTKILLSERVER    = "483"
	ERR_RESTRICTED        = "484"
	ERR_UNIQOPPRIVSNEEDED = "485"
	ERR_NOOPERHOST        = "491"
	ERR_UMODEUNKNOWNFLAG  = "501"
	ERR_USERSDONTMATCH    = "502"
)

// CommandNames is a map from command strings to their names.
var CommandNames = map[string]string{
	PASS:     "PASS",
	NICK:     "NICK",
	USER:     "USER",
	OPER:     "OPER",
	MODE:     "MODE",
	SERVICE:  "SERVICE",
	QUIT:     "QUIT",
	SQUIT:    "SQUIT",
	JOIN:     "JOIN",
	PART:     "PART",
	TOPIC:    "TOPIC",
	NAMES:    "NAMES",
	LIST:     "LIST",
	INVITE:   "INVITE",
	KICK:     "KICK",
	PRIVMSG:  "PRIVMSG",
	NOTICE:   "NOTICE",
	MOTD:     "MOTD",
	LUSERS:   "LUSERS",
	VERSION:  "VERSION",
	STATS:    "STATS",
	LINKS:    "LINKS",
	TIME:     "TIME",
	CONNECT:  "CONNECT",
	TRACE:    "TRACE",
	ADMIN:    "ADMIN",
	INFO:     "INFO",
	SERVLIST: "SERVLIST",
	SQUERY:   "SQUERY",
	WHO:      "WHO",
	WHOIS:    "WHOIS",
	WHOWAS:   "WHOWAS",
	KILL:     "KILL",
	PING:     "PING",
	PONG:     "PONG",
	ERROR:    "ERROR",
	AWAY:     "AWAY",
	REHASH:   "REHASH",
	DIE:      "DIE",
	RESTART:  "RESTART",
	SUMMON:   "SUMMON",
	USERS:    "USERS",
	WALLOPS:  "WALLOPS",
	USERHOST: "USERHOST",
	ISON:     "ISON",
	"001":    "RPL_WELCOME",
	"002":    "RPL_YOURHOST",
	"003":    "RPL_CREATED",
	"004":    "RPL_MYINFO",
	"005":    "RPL_BOUNCE",
	"302":    "RPL_USERHOST",
	"303":    "RPL_ISON",
	"301":    "RPL_AWAY",
	"305":    "RPL_UNAWAY",
	"306":    "RPL_NOWAWAY",
	"311":    "RPL_WHOISUSER",
	"312":    "RPL_WHOISSERVER",
	"313":    "RPL_WHOISOPERATOR",
	"317":    "RPL_WHOISIDLE",
	"318":    "RPL_ENDOFWHOIS",
	"319":    "RPL_WHOISCHANNELS",
	"314":    "RPL_WHOWASUSER",
	"369":    "RPL_ENDOFWHOWAS",
	"321":    "RPL_LISTSTART",
	"322":    "RPL_LIST",
	"323":    "RPL_LISTEND",
	"325":    "RPL_UNIQOPIS",
	"324":    "RPL_CHANNELMODEIS",
	"331":    "RPL_NOTOPIC",
	"332":    "RPL_TOPIC",
	"333":    "RPL_TOPICWHOTIME", // ircu specific (not in the RFC)
	"341":    "RPL_INVITING",
	"342":    "RPL_SUMMONING",
	"346":    "RPL_INVITELIST",
	"347":    "RPL_ENDOFINVITELIST",
	"348":    "RPL_EXCEPTLIST",
	"349":    "RPL_ENDOFEXCEPTLIST",
	"351":    "RPL_VERSION",
	"352":    "RPL_WHOREPLY",
	"315":    "RPL_ENDOFWHO",
	"353":    "RPL_NAMREPLY",
	"366":    "RPL_ENDOFNAMES",
	"364":    "RPL_LINKS",
	"365":    "RPL_ENDOFLINKS",
	"367":    "RPL_BANLIST",
	"368":    "RPL_ENDOFBANLIST",
	"371":    "RPL_INFO",
	"374":    "RPL_ENDOFINFO",
	"375":    "RPL_MOTDSTART",
	"372":    "RPL_MOTD",
	"376":    "RPL_ENDOFMOTD",
	"381":    "RPL_YOUREOPER",
	"382":    "RPL_REHASHING",
	"383":    "RPL_YOURESERVICE",
	"391":    "RPL_TIME",
	"392":    "RPL_USERSSTART",
	"393":    "RPL_USERS",
	"394":    "RPL_ENDOFUSERS",
	"395":    "RPL_NOUSERS",
	"200":    "RPL_TRACELINK",
	"201":    "RPL_TRACECONNECTING",
	"202":    "RPL_TRACEHANDSHAKE",
	"203":    "RPL_TRACEUNKNOWN",
	"204":    "RPL_TRACEOPERATOR",
	"205":    "RPL_TRACEUSER",
	"206":    "RPL_TRACESERVER",
	"207":    "RPL_TRACESERVICE",
	"208":    "RPL_TRACENEWTYPE",
	"209":    "RPL_TRACECLASS",
	"210":    "RPL_TRACERECONNECT",
	"261":    "RPL_TRACELOG",
	"262":    "RPL_TRACEEND",
	"211":    "RPL_STATSLINKINFO",
	"212":    "RPL_STATSCOMMANDS",
	"219":    "RPL_ENDOFSTATS",
	"242":    "RPL_STATSUPTIME",
	"243":    "RPL_STATSOLINE",
	"221":    "RPL_UMODEIS",
	"234":    "RPL_SERVLIST",
	"235":    "RPL_SERVLISTEND",
	"251":    "RPL_LUSERCLIENT",
	"252":    "RPL_LUSEROP",
	"253":    "RPL_LUSERUNKNOWN",
	"254":    "RPL_LUSERCHANNELS",
	"255":    "RPL_LUSERME",
	"256":    "RPL_ADMINME",
	"257":    "RPL_ADMINLOC",
	"258":    "RPL_ADMINLOC",
	"259":    "RPL_ADMINEMAIL",
	"263":    "RPL_TRYAGAIN",
	"401":    "ERR_NOSUCHNICK",
	"402":    "ERR_NOSUCHSERVER",
	"403":    "ERR_NOSUCHCHANNEL",
	"404":    "ERR_CANNOTSENDTOCHAN",
	"405":    "ERR_TOOMANYCHANNELS",
	"406":    "ERR_WASNOSUCHNICK",
	"407":    "ERR_TOOMANYTARGETS",
	"408":    "ERR_NOSUCHSERVICE",
	"409":    "ERR_NOORIGIN",
	"411":    "ERR_NORECIPIENT",
	"412":    "ERR_NOTEXTTOSEND",
	"413":    "ERR_NOTOPLEVEL",
	"414":    "ERR_WILDTOPLEVEL",
	"415":    "ERR_BADMASK",
	"421":    "ERR_UNKNOWNCOMMAND",
	"422":    "ERR_NOMOTD",
	"423":    "ERR_NOADMININFO",
	"424":    "ERR_FILEERROR",
	"431":    "ERR_NONICKNAMEGIVEN",
	"432":    "ERR_ERRONEUSNICKNAME",
	"433":    "ERR_NICKNAMEINUSE",
	"436":    "ERR_NICKCOLLISION",
	"437":    "ERR_UNAVAILRESOURCE",
	"441":    "ERR_USERNOTINCHANNEL",
	"442":    "ERR_NOTONCHANNEL",
	"443":    "ERR_USERONCHANNEL",
	"444":    "ERR_NOLOGIN",
	"445":    "ERR_SUMMONDISABLED",
	"446":    "ERR_USERSDISABLED",
	"451":    "ERR_NOTREGISTERED",
	"461":    "ERR_NEEDMOREPARAMS",
	"462":    "ERR_ALREADYREGISTRED",
	"463":    "ERR_NOPERMFORHOST",
	"464":    "ERR_PASSWDMISMATCH",
	"465":    "ERR_YOUREBANNEDCREEP",
	"466":    "ERR_YOUWILLBEBANNED",
	"467":    "ERR_KEYSET",
	"471":    "ERR_CHANNELISFULL",
	"472":    "ERR_UNKNOWNMODE",
	"473":    "ERR_INVITEONLYCHAN",
	"474":    "ERR_BANNEDFROMCHAN",
	"475":    "ERR_BADCHANNELKEY",
	"476":    "ERR_BADCHANMASK",
	"477":    "ERR_NOCHANMODES",
	"478":    "ERR_BANLISTFULL",
	"481":    "ERR_NOPRIVILEGES",
	"482":    "ERR_CHANOPRIVSNEEDED",
	"483":    "ERR_CANTKILLSERVER",
	"484":    "ERR_RESTRICTED",
	"485":    "ERR_UNIQOPPRIVSNEEDED",
	"491":    "ERR_NOOPERHOST",
	"501":    "ERR_UMODEUNKNOWNFLAG",
	"502":    "ERR_USERSDONTMATCH",
}
