package main

import (
	"log"
	"os"
	"time"

	"github.com/anacrolix/torrent"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Использование: %s <магнет-ссылка или путь к торрент-файлу>", os.Args[0])
	}

	torrentSource := os.Args[1]

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = "."

	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("Ошибка создания торрент-клиента: %v", err)
	}
	defer client.Close()

	// В новых версиях AddTorrent принимает строку (магнет или путь к файлу) через AddTorrentFromFile или AddMagnet
	var t *torrent.Torrent
	if _, err := os.Stat(torrentSource); err == nil {
		t, err = client.AddTorrentFromFile(torrentSource)
	} else {
		t, err = client.AddMagnet(torrentSource)
	}

	if err != nil {
		log.Fatalf("Ошибка добавления торрента: %v", err)
	}

	log.Println("Ожидание метаданных торрента...")
	<-t.GotInfo()
	log.Printf("Загрузка файла: %s", t.Name())

	for {
		stats := t.Stats()
		completed := t.BytesCompleted()
		total := t.Length()

		if total > 0 {
			percent := float64(completed) / float64(total) * 100
			// Поле ConnectedPeers заменено на TotalPeers или ActivePeers
			log.Printf("Прогресс: %.2f%% | Активные пиры: %d, Всего пиров: %d",
				percent, stats.ActivePeers, stats.TotalPeers)
		}

		// Проверка завершения через вызов метода .Complete()
		if t.Complete().Bool() {
			log.Println("Загрузка успешно завершена!")
			break
		}

		time.Sleep(3 * time.Second)
	}
}
