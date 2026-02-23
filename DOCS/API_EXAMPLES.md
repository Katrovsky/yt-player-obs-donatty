# Примеры использования API YouTube Player

## Описание

В этом документе представлены примеры использования REST API приложения YouTube Player для различных сценариев управления воспроизведением.

## Основные команды управления

### Начать воспроизведение

```bash
curl -X POST http://localhost:8093/api/play
```

Ответ:

```json
{
  "success": true,
  "message": "Playback started"
}
```

### Поставить на паузу

```bash
curl -X POST http://localhost:8093/api/pause
```

Ответ:

```json
{
  "success": true,
  "message": "Playback paused"
}
```

### Остановить воспроизведение

```bash
curl -X POST http://localhost:8093/api/stop
```

Ответ:

```json
{
  "success": true,
  "message": "Playback stopped"
}
```

### Перейти к следующему треку

```bash
curl -X POST http://localhost:8093/api/next
```

Ответ:

```json
{
  "success": true,
  "message": "Skipped to next track"
}
```

### Вернуться к предыдущему треку

```bash
curl -X POST http://localhost:8093/api/previous
```

Ответ:

```json
{
  "success": true,
  "message": "Returned to previous track"
}
```

## Управление очередью

### Добавить трек в очередь

Добавление трека по URL:

```bash
curl -X POST "http://localhost:8093/api/add-url?url=https://www.youtube.com/watch?v=dQw4w9WgXcQ&user=Viewer"
```

Добавление трека по ID:

```bash
curl -X POST "http://localhost:8093/api/add-url?id=dQw4w9WgXcQ&user=Viewer&paid=true"
```

Ответ:

```json
{
  "success": true,
  "message": "Track added to queue"
}
```

### Получить текущую очередь

```bash
curl -X GET http://localhost:8093/api/queue
```

Пример ответа:

```json
{
  "success": true,
  "data": {
    "queue": [
      {
        "video_id": "dQw4w9WgXcQ",
        "title": "Rick Astley - Never Gonna Give You Up",
        "duration_sec": 212,
        "views": 1000000,
        "added_at": "2023-01-01T12:00:00Z",
        "added_by": "Viewer",
        "is_paid": false
      }
    ],
    "current": 0,
    "state": "playing",
    "total": 1
  }
}
```

### Удалить трек из очереди

```bash
curl -X POST "http://localhost:8093/api/remove?index=0"
```

Ответ:

```json
{
  "success": true,
  "message": "Track removed from queue",
  "data": {
    "video_id": "dQw4w9WgXcQ",
    "title": "Rick Astley - Never Gonna Give You Up",
    "duration_sec": 212,
    "views": 1000000,
    "added_at": "2023-01-01T12:00:00Z",
    "added_by": "Viewer",
    "is_paid": false
  }
}
```

### Очистить очередь

```bash
curl -X POST http://localhost:8093/api/clear
```

Ответ:

```json
{
  "success": true,
  "message": "Queue cleared (5 tracks removed)"
}
```

## Управление плейлистом

### Загрузить плейлист

```bash
curl -X POST "http://localhost:8093/api/playlist/set?url=https://www.youtube.com/playlist?list=PLaeFYenjKCnMH3zUy-qt2wVRxQbxgYb-Z"
```

Ответ:

```json
{
  "success": true,
  "message": "Playlist loaded successfully",
  "data": {
    "enabled": false,
    "shuffled": false,
    "playlist_id": "PLaeFYenjKCnMH3zUy-qt2wVRxQbxgYb-Z",
    "total_tracks": 50,
    "current_index": 0
  }
}
```

### Включить плейлист

```bash
curl -X POST http://localhost:8093/api/playlist/enable
```

Ответ:

```json
{
  "success": true,
  "message": "Playlist enabled",
  "data": {
    "enabled": true,
    "shuffled": false,
    "playlist_id": "PLaeFYenjKCnMH3zUy-qt2wVRxQbxgYb-Z",
    "total_tracks": 50,
    "current_index": 0
  }
}
```

### Выключить плейлист

```bash
curl -X POST http://localhost:8093/api/playlist/disable
```

Ответ:

```json
{
  "success": true,
  "message": "Playlist disabled",
  "data": {
    "enabled": false,
    "shuffled": false,
    "playlist_id": "PLaeFYenjKCnMH3zUy-qt2wVRxQbxgYb-Z",
    "total_tracks": 50,
    "current_index": 0
  }
}
```

### Перезагрузить плейлист

```bash
curl -X POST http://localhost:8093/api/playlist/reload
```

Ответ:

```json
{
  "success": true,
  "message": "Playlist reloaded successfully",
  "data": {
    "enabled": false,
    "shuffled": false,
    "playlist_id": "PLaeFYenjKCnMH3zUy-qt2wVRxQbxgYb-Z",
    "total_tracks": 50,
    "current_index": 0
  }
}
```

### Получить треки плейлиста

```bash
curl -X GET http://localhost:8093/api/playlist/tracks
```

Пример ответа:

```json
{
  "success": true,
  "data": {
    "tracks": [
      {
        "video_id": "dQw4w9WgXcQ",
        "title": "Rick Astley - Never Gonna Give You Up",
        "duration_sec": 212,
        "views": 1000000,
        "added_at": "2023-01-01T12:00:00Z",
        "added_by": "Playlist",
        "is_paid": false
      }
    ],
    "current_index": 0,
    "total": 1
  }
}
```

### Перейти к треку в плейлисте

```bash
curl -X POST "http://localhost:8093/api/playlist/jump?index=5"
```

Ответ:

```json
{
  "success": true,
  "message": "Jumped to track"
}
```

### Перемешать треки плейлиста

```bash
curl -X POST http://localhost:8093/api/playlist/shuffle
```

Ответ:

```json
{
  "success": true,
  "message": "Playlist shuffle toggled",
  "data": {
    "enabled": false,
    "shuffled": true,
    "playlist_id": "PLaeFYenjKCnMH3zUy-qt2wVRxQbxgYb-Z",
    "total_tracks": 50,
    "current_index": 0
  }
}
```

## Получение информации

### Получить статус плеера

```bash
curl -X GET http://localhost:8093/api/status
```

Пример ответа:

```json
{
  "success": true,
  "data": {
    "state": "playing",
    "current": {
      "video_id": "dQw4w9WgXcQ",
      "title": "Rick Astley - Never Gonna Give You Up",
      "duration_sec": 212,
      "views": 1000000,
      "added_at": "2023-01-01T12:00:00Z",
      "added_by": "Viewer",
      "is_paid": false
    },
    "position": 1,
    "queue_length": 5
  }
}
```

### Получить информацию о текущем треке

```bash
curl -X GET http://localhost:8093/api/nowplaying
```

Пример ответа:

```json
{
  "success": true,
  "data": {
    "status": "playing",
    "artist": "Rick Astley",
    "title": "Never Gonna Give You Up",
    "full_title": "Rick Astley - Never Gonna Give You Up",
    "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
  }
}
```

### Получить статус мониторинга донатов

```bash
curl -X GET http://localhost:8093/api/donation/status
```

Пример ответа:

```json
{
  "success": true,
  "data": {
    "enabled": true
  }
}
```

## WebSocket соединение

Для получения обновлений в реальном времени можно использовать WebSocket соединение:

```javascript
const ws = new WebSocket('ws://localhost:8093/ws');

ws.onopen = () => {
  console.log('Connected to WebSocket');
};

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Player state updated:', data);
  // Обновление интерфейса с новыми данными
};

ws.onclose = () => {
  console.log('WebSocket connection closed');
  // Попытка переподключения через 3 секунды
  setTimeout(() => {
    connectWebSocket();
  }, 3000);
};
```

Пример сообщения от сервера:

```json
{
  "action": "playing",
  "current": {
    "video_id": "dQw4w9WgXcQ",
    "title": "Rick Astley - Never Gonna Give You Up",
    "duration_sec": 212,
    "views": 1000000,
    "added_at": "2023-01-01T12:00:00Z",
    "added_by": "Viewer",
    "is_paid": false
  },
  "queue": [
    {
      "video_id": "dQw4w9WgXcQ",
      "title": "Rick Astley - Never Gonna Give You Up",
      "duration_sec": 212,
      "views": 1000000,
      "added_at": "2023-01-01T12:00:00Z",
      "added_by": "Viewer",
      "is_paid": false
    }
  ],
  "position": 0
}
```

## Пример интеграции

### Мониторинг статуса плеера для стриминга

```javascript
// Функция для периодического получения статуса плеера
async function getPlayerStatus() {
    try {
        const response = await fetch('http://localhost:8093/api/status');
        const data = await response.json();
        
        if (data.success) {
            // Обновление информации в интерфейсе стрима
            updateStreamInfo(data.data);
        }
    } catch (error) {
        console.error('Failed to get player status:', error);
    }
}

// Обновление информации каждые 5 секунд
setInterval(getPlayerStatus, 5000);

function updateStreamInfo(status) {
    // Обновление элементов интерфейса
    document.getElementById('currentTrack').textContent = 
        status.current ? status.current.title : 'No track playing';
    document.getElementById('playerState').textContent = status.state;
    document.getElementById('queueLength').textContent = status.queue_length;
}
```
