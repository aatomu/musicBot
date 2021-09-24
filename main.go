package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
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
	sessions                = sync.Map{}
	findingUserVoiceChannel sync.Mutex
	musicDir                = "/home/pi/Public/music/"
)

type sessionItems struct {
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
	joinVC := 0
	sessions.Range(func(key interface{}, value interface{}) bool {
		joinVC++
		return true
	})
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
	filesURL := ""
	if len(m.Attachments) > 0 {
		filesURL = "Files: \""
		for _, file := range m.Attachments {
			filesURL = filesURL + file.URL + ","
		}
		filesURL = filesURL + "\"  "
	}

	//表示
	log.Print("Guild:\"" + guildName + "\"  Channel:\"" + channel.Name + "\"  " + filesURL + author + ": " + message)

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
		if userState.GuildID != m.GuildID {
			log.Println("Error : User Voicechat didn't match message channel")
			addReaction(discord, channelID, messageID, "❌")
			return
		}
		if len(m.Attachments) == 0 && len(strings.Split(message, "\n")) == 0 {
			addReaction(discord, channelID, messageID, "❌")
			return
		}
		//アップロードされたファイルから
		for _, data := range m.Attachments {
			oldMapData, _ := sessions.LoadOrStore(m.GuildID, sessionItems{
				queue:     []string{},
				conection: nil,
				skip:      0,
				loop:      false,
			})
			newMapData := oldMapData.(sessionItems)
			newMapData.queue = append(newMapData.queue, data.URL)
			sessions.Store(m.GuildID, newMapData)
		}
		//メッセージから
		text := strings.Split(message, "\n")
		text = text[1:]
		for _, data := range text {
			replace := regexp.MustCompile(`   .*$`)
			url := replace.ReplaceAllString(data, "")
			oldMapData, _ := sessions.LoadOrStore(m.GuildID, &sessionItems{
				queue:     []string{},
				conection: nil,
				skip:      0,
				loop:      false,
			})
			newMapData := oldMapData.(*sessionItems)
			newMapData.queue = append(newMapData.queue, url)
			sessions.Store(m.GuildID, newMapData)
		}

		findingUserVoiceChannel.Lock()
		defer findingUserVoiceChannel.Unlock()
		if interfaceMapData, ok := sessions.Load(m.GuildID); ok {
			mapData := interfaceMapData.(*sessionItems)
			if mapData.conection == nil {
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
		if interfaceMapData, ok := sessions.Load(m.GuildID); ok {
			mapData := interfaceMapData.(*sessionItems)
			text = text + "```"
			if mapData.loop {
				text = text + "Loop : True\n"
			} else {
				text = text + "Loop : False\n"
			}

			count := 0
			for _, url := range mapData.queue {
				count++
				replace := regexp.MustCompile(`^.*/`)
				url = replace.ReplaceAllString(url, "")
				text = text + "No." + strconv.Itoa(count) + ": " + url + "\n"
			}
			text = text + "```"
			textSplit := strings.Split(text, "")
			if len(textSplit) > 1000 {
				text = ""
				for i := 1; i < 1000; i++ {
					text = text + textSplit[i-1]
				}
				text = text + "...```"
			}
		} else {
			text = "Don't Seted Queue"
		}
		_, err := discord.ChannelMessageSend(channelID, text)
		if err != nil {
			log.Println("Error : Faild send queue message")
			log.Println(err)
			addReaction(discord, channelID, messageID, "❌")
		}
	case isPrefix(message, "skip "):
		if interfaceMapData, ok := sessions.Load(m.GuildID); ok {
			mapData := interfaceMapData.(*sessionItems)
			replace := regexp.MustCompile(`^.* `)
			countString := replace.ReplaceAllString(message, "")
			count, err := strconv.Atoi(countString)
			if err != nil {
				log.Println("Error : Faild convert countString")
				addReaction(discord, channelID, messageID, "🤔")
				return
			}

			mapData.skip = count
			addReaction(discord, channelID, messageID, "✅")

		} else {
			addReaction(discord, channelID, messageID, "❌")
		}
		return
	case isPrefix(message, "loop"):
		if interfaceMapData, ok := sessions.Load(m.GuildID); ok {
			mapData := interfaceMapData.(*sessionItems)
			mapData.loop = !mapData.loop
			if mapData.loop {
				addReaction(discord, channelID, messageID, "🔁")
			} else {
				addReaction(discord, channelID, messageID, "▶️")
			}
		} else {
			addReaction(discord, channelID, messageID, "❌")
		}
		return
	case isPrefix(message, "list"):
		privateChannel, err := discord.UserChannelCreate(authorID)
		if err != nil {
			log.Println("Error : Faild generate privateChannel")
			log.Println(err)
			addReaction(discord, channelID, messageID, "❌")
			return
		}
		list, ok := fileList(musicDir)
		list = strings.ReplaceAll(list, "//", "/")
		list = strings.ReplaceAll(list, musicDir, "")
		if ok {
			//一覧
			listArray := strings.Split(list, "\n")
			textArray := []string{"``Music List``"}
			fileType := regexp.MustCompile(`\.mp3$|\.mp4$|\.wav$`)
			//関係ないやつ削除
			for _, split := range listArray {
				if fileType.MatchString(split) {
					textArray = append(textArray, split)
				}
			}
			//送信
			text := ""
			index := 0
			for {
				for {
					text = text + "\n`" + textArray[index] + "`"
					index++
					if len(strings.Split(text, "")) > 1000 || len(textArray) == index {
						break
					}
				}

				_, err := discord.ChannelMessageSend(privateChannel.ID, text)
				if err != nil {
					log.Println(err)
					log.Println("Error : Faild send queue message")
					addReaction(discord, channelID, messageID, "❌")
					return
				}
				text = ""
				if len(textArray) == index {
					break
				}
			}
			addReaction(discord, channelID, messageID, "📄")
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
			"<discord file download link>   <コメント>```" +
			"指定されたURLのファイルを再生\n" +
			"```" + *prefix + " skip <数値>```" +
			"数値分スキップ\n" +
			"```" + *prefix + " loop```" +
			"ループするかをトグルで設定します\n" +
			"```" + *prefix + " list```" +
			"曲の一覧を表示します\n" +
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
	interfaceMapData, _ := sessions.Load(guildID)
	mapData := interfaceMapData.(*sessionItems)
	mapData.conection = vcSession

	go func() {
		for {
			interfaceMapData, _ := sessions.Load(guildID)
			mapData := interfaceMapData.(*sessionItems)
			//queueが0のとき停止
			if len(mapData.queue) == 0 {
				break
			}

			//リンクorパスの入手
			link := mapData.queue[0]
			if !strings.HasPrefix(link, "http") {
				link = musicDir + link
			}
			err := playAudioFile(mapData.conection, link, guildID)
			if err != nil {
				log.Println("Error : Faild func playAudioFile")
				log.Println(err)
				//待機
				time.Sleep(10 * time.Second)
			}

			//スキップなしで次に移動
			if mapData.skip == 0 && !mapData.loop {
				mapData.queue = mapData.queue[1:]
				continue
			}

			//スキップ判定
			if len(mapData.queue) > mapData.skip {
				mapData.queue = mapData.queue[mapData.skip:]
				mapData.skip = 0
			} else {
				break
			}
		}
		mapData.conection.Disconnect()
		sessions.Delete(guildID)
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
			interfaceMapData, _ := sessions.Load(guildID)
			mapData := interfaceMapData.(*sessionItems)
			if mapData.skip >= 1 {
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

func fileList(dir string) (list string, faild bool) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Println("Error : Faild get files in dir")
		return "", false
	}

	for _, file := range files {
		if file.IsDir() {
			data, ok := fileList(dir + "/" + file.Name())
			if !ok {
				log.Println("Error : Faild func fileList()")
				return "", false
			}
			list = list + data
			continue
		}
		list = list + dir + "/" + file.Name() + "\n"
	}
	return list, true
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
