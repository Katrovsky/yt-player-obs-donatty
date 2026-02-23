# Документация YouTube Player

Это приложение было написано для себя и поначалу представляло маленький скрипт из трёх сотен строк, поэтому его публикация не предполагалась. Сейчас же проделано много труда для того, чтобы оно стало более удобным и функциональным, и я хочу надёжно его сохранить.

## Что это

Программа для воспроизведения YouTube видео по очереди на стриме, standalone-альтернатива таким сервисам, как Trula Music. Позволяет зрителям добавлять музыкальные клипы в очередь, есть поддержка донатов через Donatty, можно загрузить плейлист. Встроенный веб-интерфейс для управления и оверлей для вставки в OBS.

**Важно:** Программа работает только с *музыкальными* видео YouTube. Другие категории воспроизводиться не будут.

## Быстрый старт

### Минимальная настройка

Для запуска программы **обязательно** нужны только два параметра:

1. **port** - порт для веб-интерфейса
2. **youtube_api_key** - ключ YouTube API

Если какие-то параметры вам не нужны, можно просто удалить эти строки.

Минимальный `config.json`:

```json
{
  "port": 8093,
  "youtube_api_key": "ВАШ_КЛЮЧ_YOUTUBE_API"
}
```

С такой конфигурацией программа запустится, но:

- Не будет ограничений на длину видео и количество просмотров
- Музыка за донаты работать не будет
- Плейлист по умолчанию не загрузится
- Очередь может быть сколь угодно большой

### Рекомендуемая настройка

Для нормальной работы лучше указать базовые ограничения:

```json
{
  "port": 8093,
  "max_duration_minutes": 9,
  "min_views": 1000,
  "max_queue_size": 100,
  "youtube_api_key": "ВАШ_КЛЮЧ_YOUTUBE_API"
}
```

### Полная настройка

Пример со всеми возможными параметрами:

```json
{
  "port": 8093,
  "max_duration_minutes": 9,
  "min_views": 1000,
  "repeat_limit": 3,
  "cleanup_after_hours": 48,
  "max_queue_size": 100,
  "donation_widget_url": "https://widgets.donatty.com/donations/?ref=ВАШ_REF&token=ВАШ_TOKEN",
  "donation_min_amount": 50,
  "youtube_api_key": "ВАШ_КЛЮЧ_YOUTUBE_API",
  "fallback_playlist_url": "https://www.youtube.com/playlist?list=ID_ПЛЕЙЛИСТА"
}
```

## Описание параметров config.json

### Обязательные параметры

- **port** (число) - порт для веб-интерфейса. По умолчанию 8093, можете указать любой свободный.
- **youtube_api_key** (строка) - ключ для YouTube Data API v3, инструкция по получению: [документация Google](https://developers.google.com/youtube/v3/getting-started). Используется для определения длительности, количества просмотров и т.д.

### Необязательные параметры

#### Ограничения на треки

- **max_duration_minutes** (число) - максимальная длина видео в минутах. 0 = без ограничений.
- **min_views** (число) - минимальное количество просмотров у видео. 0 = без ограничений.
- **repeat_limit** (число) - сколько раз подряд можно воспроизвести одно и то же видео. 0 = без ограничений.

#### Управление очередью

- **cleanup_after_hours** (число) - через сколько часов удалять старые треки. 0 = не удалять.
- **max_queue_size** (число) - максимальное количество треков в очереди. По умолчанию: 100.

#### Донаты (Donatty)

- **donation_widget_url** (строка) - ссылка на виджет уведомлений Donatty. Формат: `https://widgets.donatty.com/donations/?ref=ВАШ_REF&token=ВАШ_TOKEN`
- **donation_min_amount** (число) - минимальная сумма доната в рублях для добавления трека. По умолчанию: 50.

#### Плейлист

- **fallback_playlist_url** (строка) - ссылка на плейлист YouTube, который будет играть, когда очередь пуста.

## Настройка донатов Donatty

Программа поддерживает платформу **[Donatty](https://donatty.com/)** для автоматического добавления треков за донаты.

### Как настроить

1. Зайти в личный кабинет Donatty
2. Открыть раздел с виджетами
3. Найти виджет оповещений "Новый донат"
4. Скопировать ссылку на этот виджет
5. Вставить в `donation_widget_url` в config.json

**Важно:** Нужна именно ссылка на виджет **оповещений**, а не самостоятельно созданный виджет! Эта ссылка никогда не меняется, можно использовать постоянно.

Пример ссылки:

<https://widgets.donatty.com/donations/?ref=409b60e8-8880-4e36-b6fd-0e8XXXXX9c69&token=8uOUSDcXXXXXXXXXXXXXXXlWWnh4yz>

### Как работает

1. Зритель отправляет донат (не меньше `donation_min_amount`)
2. В сообщении доната указывает ссылку на YouTube или ID видео
3. Программа автоматически находит видео и добавляет в очередь
4. Треки от донатов имеют приоритет — играют первыми

## Запуск программы

### Обычный запуск

1. Создать файл `config.json` с настройками (можно скачать из репозитория `config.sample.json`, убрать `.sample` из названия и отредактировать)
2. Положить в каталог с программой
3. Запустить:
   - **Windows:** двойной клик на `yt-player.exe`
   - **Консоль:** `./yt-player.exe`
4. Открыть браузер на `http://localhost:8093`

### Установка как сервис

Чтобы программа запускалась автоматически при старте системы и работала в фоне.

#### Windows (через Servy)

1. Скачать [Servy](https://github.com/aelassas/servy/releases/latest)
2. После установки сконфигурируйте сервис, заполнив минимально необходимые поля:
   - Название сервиса
   - Путь к файлу `yt-player.exe`
   - Путь к каталогу (иначе приложение не сможет читать файл конфигурации, который должен лежать в том же каталоге)
   - Поочерёдно нажмите **Install**, **Start**

#### Windows (NSSM)

1. Скачать [NSSM](https://nssm.cc/download)
2. Распаковать `nssm.exe`
3. Открыть командную строку от администратора в каталоге с nssm.exe
4. Установить сервис:

   ```cmd
   nssm.exe install YouTubePlayer "C:\путь\к\yt-player.exe"
   ```

5. Настроить рабочую директорию:

   ```cmd
   nssm.exe set YouTubePlayer AppDirectory "C:\путь\к\папке"
   ```

6. Запустить сервис:

   ```cmd
   nssm.exe start YouTubePlayer
   ```

**Управление:**

- Остановить: `nssm.exe stop YouTubePlayer`
- Перезапустить: `nssm.exe restart YouTubePlayer`
- Удалить: `nssm.exe remove YouTubePlayer confirm`

#### Linux (systemd)

Для удобства установки я написал скрипт [systemd_service_creator.sh](https://github.com/Katrovsky/notes/blob/main/systemd_service_creator.sh). Справка вызывается с аргументом `-h`.

Краткий пример:

```bash
./systemd_service_creator.sh ./yt-player --name yt-player --config /path/to/config.json
```

Скрипт скопирует приложение и файл конфигурации в `/opt/yt-player`, создаст файл юнита и запустит сервис.

**Управление:**

- Проверить статус: `sudo systemctl status yt-player.service`
- Остановить: `sudo systemctl stop yt-player.service`
- Перезапустить: `sudo systemctl restart yt-player.service`
- Посмотреть логи: `sudo journalctl -u yt-player.service -f`

## Веб-интерфейс

### Главная панель — <http://localhost:8093>

Основной интерфейс для управления. Левая колонка содержит поле добавления треков и настройки плейлиста, центр — текущий трек, кнопки управления и очередь. Если fallback-плейлист загружен, справа выезжает панель с его треками.

### Оверлей для OBS — <http://localhost:8093/overlay>

Прозрачная страница с видеоплеером для вставки в OBS.

**Как добавить в OBS:**

1. Создать новый источник → Browser
2. URL: `http://localhost:8093/overlay`
3. Размер: ширина 400, высота 225 (можно больше, сохраняя пропорции 16:9)
4. Включить "Shutdown source when not visible"
5. Включить "Refresh browser when scene becomes active"

> **Важно:** оверлей прозрачен когда ничего не играет — это ожидаемое поведение. Если источник добавлен пока трек уже играл, воспроизведение начнётся только со следующего трека.

### Dok-панель для OBS — <http://localhost:8093/dock>

Компактная панель управления для встраивания в OBS как Custom Browser Dock.

**Как добавить в OBS:**

1. View → Docks → Custom Browser Docks
2. Dock Name: `YouTube Player`
3. URL: `http://localhost:8093/dock`
4. Нажать "Apply"

**Что показывает:**

- Текущий трек и следующий (если есть)
- Кнопки управления: предыдущий, играть/пауза, следующий

## Управление через интерфейс

### Добавление трека

1. Вставить ссылку YouTube или ID видео (11 символов) в поле "Add track"
2. Указать имя (опционально)
3. Нажать "Add to queue"

### Кнопки управления

- **Play/Pause** — кнопка в центре
- **Previous / Next** — стрелки
- **Remove** — появляется при наведении на трек в очереди
- **Clear all** — очистить всю очередь

### Горячие клавиши

Работают когда фокус не в поле ввода:

- **Пробел** — пауза/играть
- **→** — следующий
- **←** — предыдущий

### Управление плейлистом

1. Вставить ссылку плейлиста YouTube в поле "Fallback playlist"
2. Нажать "Load playlist" и подождать (500–800 треков загружаются около минуты)
3. Нажать "Enable" — плейлист начнёт играть когда очередь опустеет

Панель с треками плейлиста появляется справа автоматически после загрузки. Клик по треку переключает на него немедленно.

### Метки на треках

- Зелёная рамка — играет сейчас
- Золотая полоска слева — трек от доната
- Синяя полоска слева — трек из плейлиста
- Полупрозрачный — уже проигран

## API

### Управление воспроизведением

```bash
curl -X POST http://localhost:8093/api/play
curl -X POST http://localhost:8093/api/pause
curl -X POST http://localhost:8093/api/stop
curl -X POST http://localhost:8093/api/next
curl -X POST http://localhost:8093/api/previous
```

### Очередь

```bash
# Добавить трек
curl -X POST "http://localhost:8093/api/add-url?url=ССЫЛКА&user=ИМЯ"

# Добавить платный трек
curl -X POST "http://localhost:8093/api/add-url?url=ССЫЛКА&user=ИМЯ&paid=true"

# Получить очередь
curl -X GET http://localhost:8093/api/queue

# Удалить трек (index с 0, считается от текущего)
curl -X POST "http://localhost:8093/api/remove?index=0"

# Очистить всё
curl -X POST http://localhost:8093/api/clear
```

### Эндпоинты плейлиста

```bash
curl -X POST "http://localhost:8093/api/playlist/set?url=ССЫЛКА"
curl -X POST http://localhost:8093/api/playlist/enable
curl -X POST http://localhost:8093/api/playlist/disable
curl -X POST http://localhost:8093/api/playlist/reload
curl -X GET  http://localhost:8093/api/playlist/tracks
curl -X POST "http://localhost:8093/api/playlist/jump?index=5"
curl -X POST http://localhost:8093/api/playlist/shuffle
```

### Информация

```bash
curl -X GET http://localhost:8093/api/status
curl -X GET http://localhost:8093/api/nowplaying
curl -X GET http://localhost:8093/api/donation/status
```

### WebSocket

```javascript
const ws = new WebSocket('ws://localhost:8093/ws');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  // data.action   — playing / paused / stopped
  // data.current  — текущий трек
  // data.queue    — вся очередь
  // data.position — индекс текущего трека в очереди
};

ws.onclose = () => setTimeout(() => connectWebSocket(), 3000);
```

## Возможные проблемы

**Не запускается** — проверить валидность config.json, доступность порта, корректность youtube_api_key.

**Треки не добавляются** — проверить youtube_api_key и ограничения (max_duration_minutes, min_views). Программа работает только с музыкальными видео.

**Донаты не работают** — проверить donation_widget_url (должны быть ref и token), donation_min_amount. В сообщении доната должна быть ссылка на YouTube.

**Плейлист не загружается** — плейлист должен быть публичным. При большом количестве треков загрузка может занять около минуты.

**В OBS не показывается видео** — убедиться что что-то играет (оверлей прозрачен в паузе/стопе). Попробовать ПКМ → Refresh на источнике.

## Дополнительно

**Приоритет треков:** платные (донаты) → обычные → плейлист.

**Горячее обновление конфига:** при изменении config.json настройки применяются без перезапуска.

**Кэш:** информация о видео и плейлистах кэшируется на диске (`cache.db`) на 7 дней. Повторные запросы к YouTube API не выполняются до истечения TTL. При первом запуске после обновления удалите `cache.db` если видео перестали воспроизводиться.

**Музыка за баллы:** для настройки наград рекомендую [Firebot](https://firebot.app/). По вопросам настройки — Telegram-канал [Key Twitch](https://t.me/KeyTwitch).

## Скриншоты

![Dashboard](dashboard.png)
![OBS Dock](dock.png)
