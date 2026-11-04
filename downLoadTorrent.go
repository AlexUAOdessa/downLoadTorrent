// Консольный торрент-клиент на базе библиотеки github.com/anacrolix/torrent.
//
// Возможности:
//   - загрузка нескольких торрентов одновременно (magnet-ссылки и файлы .torrent);
//   - для каждого торрента отдельная строка с полосой прогресса, которая
//     обновляется НА МЕСТЕ (не добавляется новыми строками);
//   - справка по ключам /?, -h, --help или при запуске без параметров.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/anacrolix/torrent"
)

// Текст справки. %[1]s — имя запущенной программы.
const usageText = `Консольный торрент-клиент

Использование:
  %[1]s [опции] <источник> [<источник> ...]

Источник — magnet-ссылка или путь к файлу .torrent.
Можно указать несколько источников: все торренты качаются одновременно.

Опции:
  -dir  <папка>   папка для загрузок (по умолчанию ./downloads)
  -port <порт>    порт для входящих соединений (по умолчанию 51413, 0 — случайный)
  -l    <файл>    текстовый файл со списком источников (по одному в строке,
                  пустые строки и строки, начинающиеся с #, пропускаются)
  -i    <сек>     интервал обновления экрана (по умолчанию 1)
  -bar  <ширина>  ширина полосы прогресса в символах (по умолчанию 25)
  -ascii          рисовать полосу символами # и - (если █ ░ отображаются криво)
  -plain          простой вывод: каждое обновление новой строкой, без
                  полос и без управляющих ANSI-кодов (для старых консолей)
  -h, --help, /?  показать эту справку

Примеры:
  %[1]s "magnet:?xt=urn:btih:..."
  %[1]s debian.torrent "magnet:?xt=urn:btih:..." ubuntu.torrent
  %[1]s -dir D:\torrents -l list.txt
  %[1]s -port 0 -bar 40 debian.torrent

Остановка: Ctrl+C. Докачка продолжится при следующем запуске с той же папкой.
Для красивого вывода нужен терминал с поддержкой ANSI
(Windows Terminal, PowerShell 7, любой терминал Linux/macOS).
`

// printUsage выводит справку на экран.
func printUsage() {
	fmt.Printf(usageText, os.Args[0])
}

// isHelpArg определяет, является ли аргумент запросом справки.
// Стандартный пакет flag не понимает "/?", поэтому проверяем вручную.
func isHelpArg(a string) bool {
	switch strings.ToLower(a) {
	case "/?", "-?", "-h", "-help", "--help", "/h", "/help", "help":
		return true
	}
	return false
}

// readList читает источники (magnet-ссылки / пути к .torrent) из текстового файла.
func readList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var res []string
	sc := bufio.NewScanner(f)
	// Magnet-ссылки бывают очень длинными, поэтому увеличиваем буфер сканера.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		// Пропускаем пустые строки и комментарии.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res = append(res, line)
	}
	return res, sc.Err()
}

// asciiBar — рисовать полосу простыми символами (задаётся ключом -ascii).
var asciiBar bool

// item — один торрент в списке загрузок и данные для расчёта его скорости.
type item struct {
	t    *torrent.Torrent // сам торрент
	prev int64            // сколько байт было скачано при прошлом обновлении
}

// shorten обрезает строку до n символов (считая символы, а не байты,
// чтобы русские имена не ломались) и дополняет пробелами до ширины n.
// Это важно: все строки должны иметь одинаковую длину, иначе столбцы «поплывут».
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = append(r[:n-1], '…')
	}
	for len(r) < n {
		r = append(r, ' ')
	}
	return string(r)
}

// bar рисует полосу прогресса заданной ширины для процента percent (0..100).
// Заполненная часть — «█», пустая — «░».
func bar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100 * float64(width))
	full, empty := "█", "░"
	if asciiBar {
		full, empty = "#", "-"
	}
	return "[" + strings.Repeat(full, filled) + strings.Repeat(empty, width-filled) + "]"
}

// speedStr форматирует скорость: КБ/с, а если больше 1024 — МБ/с.
// Фиксированная ширина нужна, чтобы строка не «прыгала».
func speedStr(kbps float64) string {
	if kbps >= 1024 {
		return fmt.Sprintf("%6.1f МБ/с", kbps/1024)
	}
	return fmt.Sprintf("%6.0f КБ/с", kbps)
}

func main() {
	// 1. Справка. Без параметров или с ключом /? -h --help — показываем и выходим.
	//    Проверка идёт ДО flag.Parse, иначе "/?" будет воспринят как ошибка.
	if len(os.Args) < 2 {
		printUsage()
		return
	}
	for _, a := range os.Args[1:] {
		if isHelpArg(a) {
			printUsage()
			return
		}
	}

	// 2. Разбор опций командной строки.
	dir := flag.String("dir", "./downloads", "папка для загрузок")
	port := flag.Int("port", 51413, "порт")
	list := flag.String("l", "", "файл со списком источников")
	interval := flag.Float64("i", 1, "интервал обновления, сек")
	barWidth := flag.Int("bar", 25, "ширина полосы прогресса")
	plain := flag.Bool("plain", false, "простой построчный вывод")
	flag.BoolVar(&asciiBar, "ascii", false, "полоса символами # и -")
	flag.Usage = printUsage
	flag.Parse()

	// Включаем ANSI-режим консоли (нужно в Windows). Если не получилось
	// (например, вывод в файл) — переходим на простой построчный режим.
	if !*plain && !enableANSI() {
		fmt.Println("Консоль не поддерживает ANSI, включён режим -plain.")
		*plain = true
	}

	// 3. Собираем источники: из аргументов и (если задан -l) из файла.
	sources := append([]string{}, flag.Args()...)
	if *list != "" {
		items, err := readList(*list)
		if err != nil {
			log.Fatalf("Не удалось прочитать список %s: %v", *list, err)
		}
		sources = append(sources, items...)
	}
	if len(sources) == 0 {
		fmt.Println("Не указан ни один торрент.")
		fmt.Println()
		printUsage()
		os.Exit(1)
	}
	if *interval < 0.2 {
		*interval = 0.2
	}
	if *barWidth < 5 {
		*barWidth = 5
	}

	// 4. Настройка и запуск торрент-клиента. DHT и bootstrap-узлы оставляем
	//    по умолчанию — библиотека использует рабочие стандартные значения.
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = *dir
	cfg.ListenPort = *port

	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("Ошибка создания торрент-клиента: %v", err)
	}
	defer client.Close()

	// 5. Добавляем все торренты. Один клиент качает их параллельно.
	//    Порядок храним в собственном срезе: client.Torrents() отдаёт
	//    торренты в случайном порядке, и строки на экране «прыгали» бы.
	var items []*item
	for _, source := range sources {
		var t *torrent.Torrent
		var addErr error

		// Если такой файл существует — это .torrent, иначе считаем magnet-ссылкой.
		if _, statErr := os.Stat(source); statErr == nil {
			t, addErr = client.AddTorrentFromFile(source)
		} else {
			t, addErr = client.AddMagnet(source)
		}
		if addErr != nil {
			// Ошибка одного торрента не должна останавливать остальные.
			log.Printf("Ошибка добавления %s: %v", source, addErr)
			continue
		}
		items = append(items, &item{t: t})

		// ГЛАВНОЕ: DownloadAll помечает все куски к загрузке. Без этого
		// метаданные придут, а данные клиент скачивать не начнёт.
		// Ждём метаданные (GotInfo) в отдельной горутине, чтобы не блокировать
		// остальные торренты.
		go func(t *torrent.Torrent) {
			<-t.GotInfo()
			t.DownloadAll()
		}(t)
	}
	if len(items) == 0 {
		log.Fatal("Ни один торрент не добавлен.")
	}

	// 6. Корректное завершение по Ctrl+C.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	step := time.Duration(*interval * float64(time.Second))
	ticker := time.NewTicker(step)
	defer ticker.Stop()

	// printed — сколько строк мы вывели в прошлый раз. На столько строк
	// курсор поднимается вверх, чтобы перерисовать блок поверх старого.
	printed := 0

	// 7. Основной цикл: раз в interval перерисовываем блок статуса.
	for {
		select {
		case <-stop:
			fmt.Println("Остановка по сигналу...")
			return
		case <-ticker.C:
		}

		var lines []string // строки нового кадра
		var totalSpeed float64
		doneCount := 0

		for i, it := range items {
			t := it.t
			st := t.Stats()
			name := shorten(t.Name(), 22)
			prefix := fmt.Sprintf("%2d. %s ", i+1, name)

			select {
			case <-t.GotInfo():
				// Метаданные получены — размер и прогресс известны.
				done := t.BytesCompleted()
				total := t.Length()
				percent := 0.0
				if total > 0 {
					percent = float64(done) / float64(total) * 100
				}

				if t.Complete().Bool() {
					// Торрент полностью скачан.
					doneCount++
					lines = append(lines, fmt.Sprintf("%s%s %5.1f%%  готово (%.1f МБ)",
						prefix, bar(100, *barWidth), 100.0, float64(total)/(1024*1024)))
					continue
				}

				// Скорость = прирост скачанных байт за интервал.
				kbps := float64(done-it.prev) / step.Seconds() / 1024
				it.prev = done
				totalSpeed += kbps

				lines = append(lines, fmt.Sprintf("%s%s %5.1f%% %s пиры %d/%d сиды %d",
					prefix, bar(percent, *barWidth), percent, speedStr(kbps),
					st.ActivePeers, st.TotalPeers, st.ConnectedSeeders))
			default:
				// Метаданных ещё нет — размер неизвестен, показываем пустую полосу.
				lines = append(lines, fmt.Sprintf("%s%s  ...   ожидание метаданных, пиры %d/%d",
					prefix, bar(0, *barWidth), st.ActivePeers, st.TotalPeers))
			}
		}

		// Итоговая строка: сколько готово и общая скорость.
		lines = append(lines, fmt.Sprintf("Завершено %d из %d | общая скорость %s",
			doneCount, len(items), strings.TrimSpace(speedStr(totalSpeed))))

		// --- Вывод кадра ---
		if *plain {
			// Простой режим: просто печатаем строки одна за другой.
			for _, l := range lines {
				fmt.Println(time.Now().Format("15:04:05"), l)
			}
		} else {
			// Режим «на месте».
			// \033[<N>A — поднять курсор на N строк вверх (на начало прошлого блока).
			if printed > 0 {
				fmt.Printf("\033[%dA", printed)
			}
			for _, l := range lines {
				// \r — в начало строки, \033[2K — стереть строку целиком
				// (на случай, если новая короче старой), затем текст.
				fmt.Printf("\r\033[2K%s\n", l)
			}
			printed = len(lines)
		}

		// Все торренты завершены — выходим.
		if doneCount == len(items) {
			fmt.Println("Все загрузки успешно завершены!")
			return
		}
	}
}
