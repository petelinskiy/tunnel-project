# Contributing to Tunnel Proxy

Спасибо за интерес к проекту! Любой вклад приветствуется.

## Как помочь проекту

### 🐛 Сообщить о баге

1. Проверьте [Issues](https://github.com/yourusername/tunnel-project/issues) - возможно баг уже известен
2. Создайте новый Issue с меткой `bug`
3. Опишите:
   - Версию клиента/сервера
   - ОС и версию
   - Шаги для воспроизведения
   - Ожидаемое и фактическое поведение
   - Логи (если есть)

### 💡 Предложить новую функцию

1. Создайте Issue с меткой `enhancement`
2. Опишите:
   - Зачем нужна функция
   - Как она должна работать
   - Примеры использования

### 🔧 Внести код

#### Подготовка окружения

```bash
# Клонировать репозиторий
git clone https://github.com/yourusername/tunnel-project.git
cd tunnel-project

# Установить Go 1.21+
# https://go.dev/doc/install

# Установить зависимости
make install-deps

# Запустить в dev режиме
make dev-client  # В одном терминале
make dev-server  # В другом терминале
```

#### Workflow

1. **Fork** репозиторий
2. Создайте **feature branch**:
   ```bash
   git checkout -b feature/amazing-feature
   ```
3. Внесите изменения
4. Проверьте код:
   ```bash
   make fmt    # Форматирование
   make lint   # Линтинг
   make test   # Тесты
   ```
5. Commit с понятным сообщением:
   ```bash
   git commit -m "feat: add amazing feature"
   ```
6. Push в ваш fork:
   ```bash
   git push origin feature/amazing-feature
   ```
7. Создайте **Pull Request**

#### Стиль коммитов

Используйте [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` - новая функция
- `fix:` - исправление бага
- `docs:` - изменения в документации
- `style:` - форматирование кода
- `refactor:` - рефакторинг
- `test:` - добавление тестов
- `chore:` - обновление зависимостей и т.п.

Примеры:
```
feat: add latency-based load balancing
fix: resolve connection leak in tunnel manager
docs: update quickstart guide
```

### 📝 Улучшить документацию

Документация находится в:
- `README.md` - главная страница
- `docs/quickstart.md` - quick start
- Комментарии в коде

Можете:
- Исправить опечатки
- Добавить примеры
- Улучшить объяснения
- Перевести на другие языки

## Стандарты кода

### Go Code Style

Следуем [Effective Go](https://go.dev/doc/effective_go):

```go
// ✅ Хорошо
func (m *Manager) Start() error {
    if err := m.init(); err != nil {
        return fmt.Errorf("failed to initialize: %w", err)
    }
    return nil
}

// ❌ Плохо
func (m *Manager) Start() error {
    err := m.init()
    if err != nil {
        return errors.New("failed to initialize: " + err.Error())
    }
    return nil
}
```

### Комментарии

```go
// Package tunnel implements the core tunneling logic.
package tunnel

// Manager manages tunnel connections to multiple servers.
// It handles load balancing, health checks, and failover.
type Manager struct {
    config *Config
    // ... поля
}

// Start initializes and starts the tunnel manager.
// Returns an error if initialization fails.
func (m *Manager) Start() error {
    // ...
}
```

### Тестирование

Каждая новая функция должна иметь тесты:

```go
func TestManager_Start(t *testing.T) {
    tests := []struct {
        name    string
        config  *Config
        wantErr bool
    }{
        {
            name: "valid config",
            config: &Config{
                Servers: []ServerInfo{{Host: "test", Port: 443}},
            },
            wantErr: false,
        },
        {
            name:    "empty config",
            config:  &Config{},
            wantErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            m := NewManager(tt.config)
            err := m.Start()
            if (err != nil) != tt.wantErr {
                t.Errorf("Start() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Error Handling

```go
// ✅ Хорошо - добавляем контекст
if err := server.Start(); err != nil {
    return fmt.Errorf("failed to start server on port %d: %w", port, err)
}

// ❌ Плохо - теряем информацию
if err := server.Start(); err != nil {
    return err
}
```

## Pull Request Guidelines

### Checklist перед PR

- [ ] Код отформатирован (`make fmt`)
- [ ] Линтер пройден (`make lint`)
- [ ] Тесты написаны и проходят (`make test`)
- [ ] Документация обновлена (если нужно)
- [ ] Commit messages следуют стандарту
- [ ] PR description понятно объясняет изменения

### Описание PR

```markdown
## Что изменено

Краткое описание изменений.

## Зачем

Объяснение необходимости изменений.

## Как протестировать

1. Шаг 1
2. Шаг 2
3. Ожидаемый результат

## Screenshots (если UI)

![screenshot](url)

## Checklist

- [x] Тесты добавлены
- [x] Документация обновлена
- [x] Breaking changes отсутствуют (или описаны)
```

## Структура проекта

```
tunnel-project/
├── client/           # Клиентская часть
│   ├── cmd/         # Точка входа
│   ├── internal/    # Внутренняя логика
│   │   ├── tunnel/  # Туннелирование
│   │   ├── socks5/  # SOCKS5 прокси
│   │   ├── webui/   # Web UI
│   │   └── deploy/  # Автодеплой
│   └── configs/     # Конфигурация
├── server/          # Серверная часть
│   ├── cmd/
│   ├── internal/
│   └── configs/
├── shared/          # Общий код
│   ├── protocol/    # Протокол
│   ├── crypto/      # Криптография
│   └── models/      # Модели данных
└── docs/            # Документация
```

## Приоритетные задачи

Смотрите Issues с метками:
- `good first issue` - для новичков
- `help wanted` - нужна помощь
- `high priority` - критичные задачи

## Вопросы?

- Создайте Issue с вопросом
- Telegram: @tunnel_proxy_dev (если есть)
- Email: dev@example.com

Спасибо за вклад! 🎉
