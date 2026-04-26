# beakid-go

En: Lock-free unique ID generator inspired by Twitter Snowflake. Designed for high-throughput services where allocating a UUID per request is too slow.

Ru: Генератор уникальных ID без блокировок, вдохновлённый Twitter Snowflake. Создан для высоконагруженных сервисов, где аллоцировать UUID на каждый запрос слишком дорого.

---

## ID layout

| Bits     | Field      | Size    | Description                                 |
|----------|------------|---------|---------------------------------------------|
| [63..29] | timestamp  | 35 bits | 100 ms units since epoch (~109 years range) |
| [28..10] | sequence   | 19 bits | up to 524 288 IDs per window                |
| [9..0]   | worker_id  | 10 bits | 0..=1023, distinguishes generator instances |

En: IDs are monotonically increasing within a single `Generator` instance and sortable by creation time across the cluster within the same 100 ms window.

Ru: ID монотонно возрастают в рамках одного экземпляра `Generator` и сортируемы по времени создания в кластере в рамках одного 100 мс окна.

---

## How it works

En: The generator is created with a unique `workerID` and is safe to share across goroutines via a pointer. ID generation reduces to a single `Add` on an `atomic.Uint64` — no mutex, no system call.

For the generator to function, a background goroutine must call `UpdateTime` every 100 ms.

Each 100 ms window holds up to 524 288 IDs, giving a sustained throughput of ~5.2 M IDs/s. Virtual time windows allow the generator to absorb burst traffic by borrowing time from the future. The virtual clock can run at most 1 second ahead of real time. If it goes too far, the generator blocks — preventing the virtual clock from drifting indefinitely.

Ru: Генератор создаётся с уникальным `workerID` и рассчитан на расшаривание между горутинами через указатель. Генерация ID сводится к одному `Add` на `atomic.Uint64` — без мьютексов, без системных вызовов.

Для работы генератора обязательно должна быть запущена отдельная горутина, вызывающая `UpdateTime` каждые 100 мс.

Каждое 100 мс окно вмещает до 524 288 ID, что даёт устойчивую пропускную способность ~5.2 млн ID/с. Виртуальные временные окна позволяют пережить всплески активности, занимая время в будущем. Виртуальные часы могут уйти максимум на 1 секунду вперёд реального времени. Если они уходят слишком далеко — генератор блокируется, не позволяя виртуальному времени убегать бесконечно.

---

## Usage

En: Use `Run` to create the generator and start the background `UpdateTime` worker automatically:

Ru: Используйте `Run` для создания генератора и автоматического запуска фонового воркера `UpdateTime`:

```go
generator, stop, err := beakid.Run(workerID, time.UnixMilli(0).UTC())
if err != nil {
    log.Fatal(err)
}
defer stop() // stops the background goroutine
```

En: In case of blocking, `Generate` returns `ErrBlocked`. You can handle it yourself, or use `MustGenerate` which spins until an ID is available:

Ru: В случае блокировки `Generate` вернёт `ErrBlocked`. Вы можете обработать ошибку самостоятельно, либо использовать `MustGenerate`, который крутится в цикле до получения ID:

```go
// handle blocking manually / обработка блокировки вручную
id, err := generator.Generate()
if errors.Is(err, beakid.ErrBlocked) {
    // retry later / повторить позже
}

// or spin until available / или ждать пока не разблокируется
id := generator.MustGenerate()
```

---

En: For manual setup without `Run`, call `UpdateTime` once before generating IDs and then every 100 ms:

Ru: Для ручной настройки без `Run` вызовите `UpdateTime` один раз перед генерацией ID, а затем каждые 100 мс:

```go
generator, err := beakid.TryNew(workerID, time.UnixMilli(0).UTC())
if err != nil {
    log.Fatal(err)
}
generator.UpdateTime() // initialise / инициализация

ticker := time.NewTicker(100 * time.Millisecond)
go func() {
    for range ticker.C {
        generator.UpdateTime()
    }
}()
```

---

## Base62

En: Every `BeakId` can be encoded as a fixed 11-character base62 string (`0–9`, `A–Z`, `a–z`) for use in URLs, database keys, or logs.

Ru: Каждый `BeakId` можно закодировать в фиксированную 11-символьную строку base62 (`0–9`, `A–Z`, `a–z`) для использования в URL, ключах БД или логах.

```go
s := id.Base62()
id2, err := beakid.FromBase62(s)
// id2 == id
```

---

## Known limitations

En:
- **workerID must be unique across the deployment.** Two generators with the same `workerID` running simultaneously will produce duplicate IDs. Coordination (e.g. via Redis, etcd, or static config) is the caller's responsibility.
- **Clock skew.** If the system clock jumps backward, IDs generated after the jump can collide with or sort before existing IDs. The generator does not detect backward clock jumps.
- **Cache line contention under many goroutines.** All goroutines increment the same `atomic.Uint64`. On NUMA systems or with many cores, MESI cache coherence traffic can reduce throughput.

Ru:
- **workerID должен быть уникален в рамках всего деплоя.** Два генератора с одинаковым `workerID`, работающие одновременно, будут создавать дубликаты. Координация (например, через Redis, etcd или статичный конфиг) — ответственность вызывающего.
- **Drift часов.** Если системные часы прыгают назад, ID, сгенерированные после прыжка, могут совпасть с существующими или нарушить порядок сортировки. Генератор не обнаруживает прыжки назад.
- **Конкуренция за кэш-линию при большом числе горутин.** Все горутины инкрементируют один `atomic.Uint64`. На NUMA-системах или при большом числе ядер трафик когерентности кэша (MESI) снижает пропускную способность.

---

## License

MIT
