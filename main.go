package main

import (
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/atomu21263/atomicgo"
	"github.com/atomu21263/slashlib"
	"github.com/bwmarrin/discordgo"
)

var (
	//変数定義
	token    = flag.String("token", "", "bot token")
	sessions = map[string]*sessionItems{}
	mapWrite sync.Mutex
	musicDir = "/home/pi/Public/music/"
)

type sessionItems struct {
	queue []string
	skip  int64
	loop  bool
}

func main() {
	//flag入手
	flag.Parse()
	fmt.Println("token        :", *token)

	//bot起動準備
	discord := atomicgo.DiscordBotSetup(*token)

	//eventトリガー設定
	discord.AddHandler(onReady)
	discord.AddHandler(onInteractionCreate)

	//起動
	atomicgo.DiscordBotStart(discord)
	defer atomicgo.DiscordBotEnd(discord)

	//bot停止対策
	atomicgo.StopWait()
}

//BOTの準備が終わったときにCall
func onReady(discord *discordgo.Session, r *discordgo.Ready) {
	//起動メッセージ表示
	fmt.Println("Listening...")
	// コマンド生成
	cmd := slashlib.Command{}
	cmd.
		AddCommand("add", "曲を追加").
		AddOption(slashlib.TypeFile, "file", "ファイルで音楽を追加", false, 0, 0).
		AddOption(slashlib.TypeString, "path", "パスで音楽を追加", false, 0, 0).
		AddCommand("skip", "曲をスキップ").
		AddOption(slashlib.TypeInt, "amount", "数値分 スキップ", true, 0, 0).
		AddCommand("loop", "現在再生中の曲をループする").
		AddCommand("list", "現在登録されている曲一覧を表示").
		AddCommand("queue", "再生中,再生待ち の曲を表示").
		CommandCreate(discord, "")
	//10秒に1回呼び出す
	oneSecTicker := time.NewTicker(10 * time.Second)
	go func() {
		for {
			<-oneSecTicker.C
			JoinedServers := len(discord.State.Guilds)
			JoinedVC := 0
			for range sessions {
				JoinedVC++
			}
			JoinVC := ""
			if JoinedVC > 1 {
				JoinVC = fmt.Sprintf("%d鯖で再生中", JoinedVC)
			}
			atomicgo.BotStateUpdate(discord, fmt.Sprintf("/add | %d鯖で稼働中 %s", JoinedServers, JoinVC), 0)
		}
	}()
}

// slashCommand受信
func onInteractionCreate(discord *discordgo.Session, iCreate *discordgo.InteractionCreate) {
	i := slashlib.InteractionViewAndEdit(discord, iCreate)

	// 念のためチェック
	if i.Check != slashlib.SlashCommand {
		return
	}

	// レスポンス用でーた
	res := slashlib.InteractionResponse{
		Discord:     discord,
		Interaction: iCreate.Interaction,
	}
	// 分岐
	switch i.Command.Name {
	case "add":
		//ユーザーのVCデータ入手
		userState := atomicgo.UserVCState(discord, i.UserData.ID)
		//入ってないならreturn
		if userState == nil {
			ReturnResponse(res, "Failed", "Voice Chatにいないため実行できません。", false)
			return
		}
		//メッセージの鯖と違うならreturn
		if userState.GuildID != i.GuildID {
			ReturnResponse(res, "Failed", "サーバーが異なるため実行できません。", false)
			return
		}
		//ファイルがない or 曲名指定なしでreturn
		if len(i.Command.Options) == 0 {
			ReturnResponse(res, "Failed", "ファイル/パス が設定されていません。", false)
			return
		}
		songs := []string{}
		for key, s := range i.CommandOptions {
			switch key {
			case "file":
				songs = append(songs, s.AttachmentValue(i).URL)
			case "path":
				songs = append(songs, s.StringValue())
			}
		}
		if _, ok := sessions[i.GuildID]; !ok {
			mapWrite.Lock()
			sessions[i.GuildID] = &sessionItems{
				queue: songs,
				skip:  0,
				loop:  false,
			}
			mapWrite.Unlock()
			joinUserVoiceChannel(discord, res, i.GuildID, userState)
		} else {
			mapWrite.Lock()
			sessions[i.GuildID].queue = append(sessions[i.GuildID].queue, songs...)
			ReturnResponse(res, "Command Success", "This Song Appned Queue.", false)
			mapWrite.Unlock()
		}

		return

	case "skip":
		// mapチェック
		mapData, ok := mapCheck(i.GuildID, res)
		if !ok {
			return
		}
		//スキップを設定
		mapData.skip = i.Command.Options[0].IntValue()
		// 書き込み
		mapWrite.Lock()
		sessions[i.GuildID] = mapData
		mapWrite.Unlock()
		//送信
		ReturnResponse(res, "Command Success", fmt.Sprintf("Music Skipped %d", mapData.skip), true)
		return
	case "loop":
		// mapチェック
		mapData, ok := mapCheck(i.GuildID, res)
		if !ok {
			return
		}
		//反転
		mapData.loop = !mapData.loop
		// 書き込み
		mapWrite.Lock()
		sessions[i.GuildID] = mapData
		mapWrite.Unlock()
		//送信
		ReturnResponse(res, "Command Success", fmt.Sprintf("Music Loop %t", mapData.loop), true)
		return
	case "list":
		//DMのチャンネルIDを入手 or 生成
		privateChannel, err := discord.UserChannelCreate(i.UserData.ID)
		if atomicgo.PrintError("Failed attend private channel", err) {
			ReturnResponse(res, "Failed", "Direct Messageを送信できませんでした。", false)
			return
		}
		//ファイルの一覧を入手
		list, ok := atomicgo.FileList(musicDir)
		for i := 0; i < len(list); i++ {
			list[i] = strings.ReplaceAll(list[i], musicDir, "")
		}
		// Embedの配列
		embeds := []*discordgo.MessageEmbed{}
		//入手成功したら
		if ok {
			//一覧
			textArray := []string{"``Music List``"}
			// 必要なのを保存
			for _, line := range list {
				if atomicgo.StringCheck(line, `\.mp3$|\.mp4$|\.wav$`) {
					textArray = append(textArray, line)
				}
			}
			//送信
			text := ""
			index := 0
			for index < len(textArray) {
				for index < len(textArray) {
					text = text + "\n`" + textArray[index] + "`"
					index++

					//文字数オーバー回避
					if len(strings.Split(text, "")) > 2000 {
						break
					}
				}
				//送信
				atomicgo.SendEmbed(discord, privateChannel.ID, &discordgo.MessageEmbed{
					Title:       "Music List",
					Description: text,
					Color:       0xff1111,
				})
				//リセット
				text = ""
				// embedsの確認
				if len(embeds) > 25 {
					index = 10000
					break
				}
			}
		}
		ReturnResponse(res, "Command Success", "Sended Your Direct Message", false)
		return
	case "queue":
		// mapチェック
		mapData, ok := mapCheck(i.GuildID, res)
		if !ok {
			return
		}
		// 返す用のEmbed
		embed := &discordgo.MessageEmbed{
			Title:       "Queue",
			Description: "",
			Color:       0xff1111,
		}
		// コードブロック化
		embed.Description += "```"
		// ループ設定
		if mapData.loop {
			embed.Description += "Loop : True\n"
		} else {
			embed.Description += "Loop : False\n"
		}
		// キューの数を設定
		embed.Description += fmt.Sprintf("Queue : %d\n", len(mapData.queue))

		// キューの一覧
		for i := 0; i < len(mapData.queue); i++ {
			url := atomicgo.StringReplace(mapData.queue[i], "", "^.*/")
			// 保存
			embed.Description += fmt.Sprintf("No.%d: %s\n", i+1, url)
		}
		//閉じる
		embed.Description += "```"

		// 文字切り取り
		if len(strings.Split(embed.Description, "")) > 4000 {
			embed.Description = atomicgo.StringCut(embed.Description, 4000)
			embed.Description += "...```"
		}
		//送信
		res.Reply(&discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				embed,
			},
			Flags: slashlib.Invisible,
		})
		return
	}
}

// mapチェック
func mapCheck(key string, res slashlib.InteractionResponse) (session *sessionItems, ok bool) {
	mapData, ok := sessions[key]
	if !ok {
		ReturnResponse(res, "Failed", "データを見つけることができませんでした...", false)
		return &sessionItems{}, false
	}
	return mapData, true
}

func ReturnResponse(res slashlib.InteractionResponse, title, description string, visible bool) {
	responseData := &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       title,
				Description: description,
			},
		},
	}
	if !visible {
		responseData.Flags = slashlib.Invisible
	}
	res.Reply(responseData)
}

func joinUserVoiceChannel(discord *discordgo.Session, res slashlib.InteractionResponse, guildID string, vcConnection *discordgo.VoiceState) {
	ReturnResponse(res, "Command Running..", "Voice Chat Connecting...", false)
	//VCに接続
	vcSession, err := discord.ChannelVoiceJoin(vcConnection.GuildID, vcConnection.ChannelID, false, true)
	if atomicgo.PrintError("Failed join VC", err) {
		res.Edit(&discordgo.WebhookEdit{
			Embeds: []*discordgo.MessageEmbed{
				{
					Title:       "Failed",
					Description: "ボイスチャットへの接続に失敗しました.",
				},
			},
		})
		return
	}
	res.Edit(&discordgo.WebhookEdit{
		Embeds: []*discordgo.MessageEmbed{
			{
				Title:       "Command Success",
				Description: "曲の再生をします",
			},
		},
	})

	//go funcでルーチン化して並列処理
	go func() {
		for {
			//更新を受け取るため毎回読み込み
			session, _ := mapCheck(guildID, slashlib.InteractionResponse{})

			//queueが0のとき停止
			if len(session.queue) == 0 {
				break
			}

			//リンクorパスの入手
			link := session.queue[0]
			if !strings.HasPrefix(link, "http") {
				link = musicDir + link
			}
			//再生
			err := atomicgo.PlayAudioFile(1, 1, vcSession, link)
			if err != nil {
				//再生をあきらめる
				break
			}

			//スキップなしで次に移動
			if session.skip == 0 && !session.loop {
				mapWrite.Lock()
				session.queue = session.queue[1:]
				mapWrite.Unlock()
				continue
			}

			//スキップ判定
			if len(session.queue) > int(session.skip) {
				session.queue = session.queue[session.skip:]
				session.skip = 0
			} else {
				// 曲数よりskip数が多いから終了
				break
			}
		}

		//終了処理
		vcSession.Disconnect()
		mapWrite.Lock()
		delete(sessions, guildID)
		mapWrite.Unlock()
	}()
}
