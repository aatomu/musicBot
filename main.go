package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jonas747/dca"
)

var (
	//変数定義
	prefix                  = flag.String("prefix", "", "call prefix")
	token                   = flag.String("token", "", "bot token")
	joinedServer            = map[string]*vcSessionItems{}
	findingUserVoiceChannel sync.Mutex
	folder                  = "/home/pi/Public/cloud/"
)

type vcSessionItems struct {
	conection *discordgo.VoiceConnection
	queue     []string
	skip      int
	loop      bool
}

func main() {
	//flag入手
	flag.Parse()
	fmt.Println("prefix       :", *prefix)
	fmt.Println("token        :", *token)

	//bot起動準備
	discord, err := discordgo.New()
	if err != nil {
		fmt.Println("Error logging")
	}

	//token入手
	discord.Token = "Bot " + *token

	//eventトリガー設定
	discord.AddHandler(onReady)
	discord.AddHandler(onMessageCreate)

	//起動
	if err = discord.Open(); err != nil {
		fmt.Println(err)
	}
	defer func() {
		if err := discord.Close(); err != nil {
			log.Println(err)
		}
	}()
	//起動メッセージ表示
	fmt.Println("Listening...")

	//bot停止対策
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

}

//BOTの準備が終わったときにCall
func onReady(discord *discordgo.Session, r *discordgo.Ready) {
	//1秒に1回呼び出す
	oneSecTicker := time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-oneSecTicker.C:
				botStateUpdate(discord)
			}
		}
	}()
}

func botStateUpdate(discord *discordgo.Session) {
	//botのステータスアップデート
	joinServer := len(discord.State.Guilds)
	joinVC := len(joinedServer)
	VC := ""
	if joinVC != 0 {
		VC = " " + strconv.Itoa(joinVC) + "鯖で再生中"
	}
	state := discordgo.UpdateStatusData{
		Activities: []*discordgo.Activity{
			{
				Name: *prefix + " help | " + strconv.Itoa(joinServer) + "鯖で稼働中" + VC,
				Type: 0,
			},
		},
		AFK:    false,
		Status: "online",
	}
	discord.UpdateStatusComplex(state)
}

//メッセージが送られたときにCall
func onMessageCreate(discord *discordgo.Session, m *discordgo.MessageCreate) {
	//一時変数
	guildID := m.GuildID
	guildData, err := discord.Guild(guildID)
	guildName := ""
	if err == nil {
		guildName = guildData.Name
	} else {
		guildName = "DirectMessage"
	}
	channelID := m.ChannelID
	channel, _ := discord.Channel(channelID)
	message := m.Content
	messageID := m.ID
	author := m.Author.Username
	authorID := m.Author.ID

	//表示
	log.Print("Guild:\"" + guildName + "\"  Channel:\"" + channel.Name + "\"  " + author + ": " + message)

	//bot 読み上げ無し のチェック
	if m.Author.Bot {
		return
	}

	switch {
	case isPrefix(message, "add"):
		userState := findUserVoiceState(discord, authorID)
		if userState.GuildID == "" {
			log.Println("Error : User didn't join Voicechat")
			addReaction(discord, channelID, messageID, "❌")
			return
		}
		if len(m.Attachments) == 0 && len(strings.Split(message, "\n")) == 0 {
			addReaction(discord, channelID, messageID, "❌")
			return
		}
		//アップロードされたファイルから
		for _, data := range m.Attachments {
			if _, ok := joinedServer[userState.GuildID]; ok {
				joinedServer[userState.GuildID].queue = append(joinedServer[userState.GuildID].queue, data.URL)
			} else {
				joinedServer[userState.GuildID] = &vcSessionItems{
					queue:     []string{data.URL},
					conection: nil,
					skip:      0,
					loop:      false,
				}
			}
		}
		//メッセージから
		text := strings.Split(message, "\n")
		text = text[1:]
		for _, data := range text {
			replace := regexp.MustCompile(` .*$`)
			url := replace.ReplaceAllString(data, "")
			if _, ok := joinedServer[userState.GuildID]; ok {
				joinedServer[userState.GuildID].queue = append(joinedServer[userState.GuildID].queue, url)
			} else {
				joinedServer[userState.GuildID] = &vcSessionItems{
					queue:     []string{url},
					conection: nil,
				}
			}
		}

		findingUserVoiceChannel.Lock()
		defer findingUserVoiceChannel.Unlock()
		if _, ok := joinedServer[userState.GuildID]; ok {
			vcSession := joinedServer[userState.GuildID].conection
			if vcSession == nil {
				joinUserVoiceChannel(discord, messageID, channelID, guildID, userState)
				addReaction(discord, channelID, messageID, "🎶")
				return
			}
			addReaction(discord, channelID, messageID, "🎵")
			return
		}
		addReaction(discord, channelID, messageID, "❌")
		return
	case isPrefix(message, "q"):
		text := ""
		if _, ok := joinedServer[guildID]; ok {
			text = text + "```"
			if joinedServer[guildID].loop {
				text = text + "Loop : True\n"
			} else {
				text = text + "Loop : False\n"
			}

			count := 0
			for _, url := range joinedServer[guildID].queue {
				count++
				replace := regexp.MustCompile(`^.*/`)
				url = replace.ReplaceAllString(url, "")
				text = text + "No." + strconv.Itoa(count) + ": " + url + "\n"
			}
			text = text + "```"
		} else {
			text = "Don't Seted Queue"
		}
		_, err := discord.ChannelMessageSend(channelID, text)
		if err != nil {
			log.Println(err)
			log.Println("Error : Faild send queue message")
			addReaction(discord, channelID, messageID, "❌")
		}
	case isPrefix(message, "skip "):
		replace := regexp.MustCompile(`^.* `)
		countString := replace.ReplaceAllString(message, "")
		count, err := strconv.Atoi(countString)
		if err != nil {
			log.Println("Error : Faild convert countString")
			addReaction(discord, channelID, messageID, "🤔")
			return
		}
		joinedServer[guildID].skip = count
		addReaction(discord, channelID, messageID, "✅")
		return
	case isPrefix(message, "loop"):
		if _, ok := joinedServer[guildID]; ok {
			joinedServer[guildID].loop = !joinedServer[guildID].loop
			if joinedServer[guildID].loop {
				addReaction(discord, channelID, messageID, "🔁")
			} else {
				addReaction(discord, channelID, messageID, "▶️")
			}
		} else {
			addReaction(discord, channelID, messageID, "❌")
		}
		return
	case isPrefix(message, "help"):
		text := "```Music Help```" +
			"```" + *prefix + " help```" +
			"ヘルプを表示\n" +
			"```" + *prefix + " add```" +
			"ファイルを再生 *ファイルアップロード時に\n" +
			"```" + *prefix + " add\n" +
			"<discord file download link>\n" +
			"<discord file download link> <コメント>```" +
			"指定されたURLのファイルを再生\n" +
			"```" + *prefix + " skip <数値>```" +
			"数値分スキップ\n" +
			"```" + *prefix + " loop```" +
			"ループするかをトグルで設定します\n" +
			"```" + *prefix + " q```" +
			"キューを表示 (No.1が現在再生中の曲)\n"
		_, err := discord.ChannelMessageSend(channelID, text)
		if err != nil {
			log.Println(err)
			log.Println("Error : Faild send queue message")
			addReaction(discord, channelID, messageID, "❌")
		}
	}
}

func isPrefix(message string, check string) bool {
	return strings.HasPrefix(message, *prefix+" "+check)
}

func joinUserVoiceChannel(discord *discordgo.Session, messageID string, channelID string, guildID string, vcConnection *discordgo.VoiceState) {
	vcSession, err := discord.ChannelVoiceJoin(vcConnection.GuildID, vcConnection.ChannelID, false, true)
	if err != nil {
		log.Println("Error : Failed join vc")
		addReaction(discord, channelID, messageID, "❌")
		return
	}
	joinedServer[guildID].conection = vcSession

	go func() {
		for len(joinedServer[guildID].queue) > 0 {
			link := joinedServer[guildID].queue[0]
			if !strings.HasPrefix(link, "http") {
				link = folder + link
			}
			err := playAudioFile(joinedServer[guildID].conection, link, guildID)

			//エラー回収
			if err != nil {
				log.Println("Error : Faild func playAudioFile")
				log.Println(err)
				delete(joinedServer, guildID)
				return
			}

			//スキップなしで次に移動
			if joinedServer[guildID].skip == 0 && !joinedServer[guildID].loop {
				joinedServer[guildID].queue = joinedServer[guildID].queue[1:]
				continue
			}

			//スキップ判定
			if len(joinedServer[guildID].queue) > joinedServer[guildID].skip {
				joinedServer[guildID].queue = joinedServer[guildID].queue[joinedServer[guildID].skip:]
				joinedServer[guildID].skip = 0
			} else {
				joinedServer[guildID].queue = []string{}
			}
		}
		joinedServer[guildID].conection.Disconnect()
		delete(joinedServer, guildID)
	}()
}

func findUserVoiceState(discord *discordgo.Session, userid string) *discordgo.VoiceState {
	for _, guild := range discord.State.Guilds {
		for _, vs := range guild.VoiceStates {
			if vs.UserID == userid {
				return vs
			}
		}
	}
	return nil
}

func playAudioFile(vcsession *discordgo.VoiceConnection, filename string, guildID string) error {
	if err := vcsession.Speaking(true); err != nil {
		return err
	}
	defer vcsession.Speaking(false)

	opts := dca.StdEncodeOptions
	opts.CompressionLevel = 0
	opts.RawOutput = true
	opts.Bitrate = 120
	encodeSession, err := dca.EncodeFile(filename, opts)
	if err != nil {
		log.Println("Error : Faild encode file")
		return err
	}

	done := make(chan error)
	stream := dca.NewStream(encodeSession, vcsession, done)
	ticker := time.NewTicker(5 * time.Second)

	for {
		select {
		case err := <-done:
			if err != nil && err != io.EOF {
				return err
			}
			encodeSession.Cleanup()
			return nil
		case <-ticker.C:
			playbackPosition := stream.PlaybackPosition()
			log.Println("Sending Now... : Playback:" + fmt.Sprint(playbackPosition))
			if playbackPosition == 0 {
				log.Println("Error : Faild play music")
				encodeSession.Cleanup()
				_, err := stream.Finished()
				if err != nil {
					log.Println(err)
					log.Println("Error : Faild stop play music")
				}
				return nil
			}
		default:
			if joinedServer[guildID].skip >= 1 {
				encodeSession.Cleanup()
				_, err := stream.Finished()
				if err != nil {
					log.Println(err)
					log.Println("Error : Faild stop play music")
				}
				return nil
			}

		}
	}
}

//リアクション追加用
func addReaction(discord *discordgo.Session, channelID string, messageID string, reaction string) {
	err := discord.MessageReactionAdd(channelID, messageID, reaction)
	if err != nil {
		log.Print("Error: addReaction Failed")
		log.Println(err)
		return
	}
}
