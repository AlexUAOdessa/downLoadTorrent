package main

import (
	"log"
	"os"
	"time"

	"github.com/anacrolix/torrent"
)

func main() {
	// Проверяем, передал ли пользователь хотя бы одну ссылку или файл в аргументах
	if len(os.Args) < 2 {
		log.Fatalf("Использование: %s <ссылка_1> <ссылка_2> ...", os.Args[0])
	}

	// Инициализируем конфигурацию торрент-клиента по умолчанию
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = "./downloads" // Указываем единую папку для сохранения всех загрузок

	// Создаем новый экземпляр торрент-клиента
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("Ошибка создания торрент-клиента: %v", err)
	}
	defer client.Close() // Гарантированно закрываем клиент при выходе из функции

	// Проходим циклом по всем аргументам командной строки (начиная со второго)
	for _, source := range os.Args[1:] {
		var t *torrent.Torrent
		var addErr error

		// Проверяем, является ли источник локальным .torrent файлом или магнет-ссылкой
		if _, statErr := os.Stat(source); statErr == nil {
			t, addErr = client.AddTorrentFromFile(source)
		} else {
			t, addErr = client.AddMagnet(source)
		}

		// Если при добавлении возникла ошибка, логируем её и переходим к следующему торренту
		if addErr != nil {
			log.Printf("Ошибка добавления торрента %s: %v", source, addErr)
			continue
		}

		// Используем переменную t, чтобы компилятор не выдавал ошибку "declared and not used"
		log.Printf("Торрент успешно добавлен в очередь: %s", source)
		_ = t // Явное указание компилятору, что переменная задействована
	}

	// Бесконечный цикл мониторинга прогресса всех активных загрузок
	for {
		torrents := client.Torrents()
		allComplete := true

		// Проверяем состояние каждого добавленного торрента
		for _, t := range torrents {
			select {
			case <-t.GotInfo():
				// Метаданные получены, вычисляем процент выполнения
				completed := t.BytesCompleted()
				total := t.Length()
				if total > 0 {
					percent := float64(completed) / float64(total) * 100
					log.Printf("[%s] Прогресс: %.2f%% | Активные пиры: %d", t.Name(), percent, t.Stats().ActivePeers)
				}
			default:
				// Если метаданные еще не загрузились из сети
				log.Printf("Ожидание метаданных для торрента...")
			}

			// Если хотя бы один торрент еще не скачан до конца, флаг общей готовности сбрасывается
			if !t.Complete().Bool() {
				allComplete = false
			}
		}

		// Если все торренты скачаны и список не пуст — завершаем работу программы
		if allComplete && len(torrents) > 0 {
			log.Println("Все загрузки успешно завершены!")
			break
		}

		// Пауза в 3 секунды перед следующим циклом проверки
		time.Sleep(3 * time.Second)
	}
}
