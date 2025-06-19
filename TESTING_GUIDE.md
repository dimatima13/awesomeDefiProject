# Полная инструкция по тестированию Raydium Swap Tool

## 1. Подготовка окружения

```bash
# Перейдите в директорию проекта
cd /Users/dmitrijtimosenko/GolandProjects/awesomeProject

# Установите зависимости
go mod tidy

# Соберите проект
go build main.go
```

## 2. Подготовка кошелька

### Вариант А: Использовать тестовый кошелёк (рекомендуется)

```bash
# Создайте новый кошелёк используя Solana CLI
solana-keygen new --no-bip39-passphrase

# Или используйте готовый тестовый приватный ключ (НЕ ИСПОЛЬЗУЙТЕ для реальных средств!)
# Пример тестового ключа (публичный адрес: 5ZiE3vAkrdXBgyFL7KqG3RoEGBws4CjRcXVbABDLZTgx)
export SOLANA_PRIVATE_KEY=3bsbhwanLnyjnZ3t4gHKkR8RMdaL3vHTDrsHPqDW4stkG5S4temxqewKr3VfumdfT5p8YRUYMAUfna3xPCeNxmtE
```

### Вариант Б: Экспорт из Phantom Wallet

1. Откройте Phantom Wallet
2. Настройки → Безопасность → Показать приватный ключ
3. Скопируйте ключ и установите:
```bash
export SOLANA_PRIVATE_KEY=ваш_приватный_ключ_здесь
```

## 3. Пополнение кошелька

Для тестов вам понадобится минимум 0.01 SOL. Получите адрес кошелька:

```bash
# Если использовали тестовый ключ выше
echo "Адрес: 5ZiE3vAkrdXBgyFL7KqG3RoEGBws4CjRcXVbABDLZTgx"

# Или для вашего ключа
./main -execute 2>&1 | grep "Wallet loaded" | cut -d' ' -f3
```

Переведите минимум 0.01 SOL на этот адрес.

## 4. Тестирование котировок (безопасно)

```bash
# Тест 1: Котировка покупки BONK за 0.001 SOL
./main -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 0.001 -side buy

# Тест 2: Поиск пула по токену USDC
./main -token EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v -amount 0.001 -side buy

# Тест 3: Котировка продажи (если у вас есть BONK)
./main -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 1000000 -side sell
```

## 5. Выполнение тестового свопа

### Минимальный тест - покупка BONK за 0.001 SOL

```bash
./main -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 0.001 -side buy -execute
```

### Пошаговый процесс:

1. **Загрузка кошелька**
```
Wallet loaded: 5ZiE3vAkrdXBgyFL7KqG3RoEGBws4CjRcXVbABDLZTgx
```

2. **Поиск пула** (может занять 10-30 секунд)
```
Searching for pools on-chain using getProgramAccounts...
This may take 10-30 seconds...
```

3. **Информация о пуле**
```
=== Pool Information (On-Chain) ===
Pool Address: 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2
Base Token: So11111111111111111111111111111111111111112 (decimals: 9)
Quote Token: DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263 (decimals: 5)
Base Reserve: 123.456789
Quote Reserve: 987654321.12345
```

4. **Результат котировки**
```
=== QUOTE RESULT ===
Protocol: Raydium V4 AMM (Pure On-Chain)
Pool: 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2
Operation: BUY
Amount In: 0.001000000
Expected Out: 78965.43210
====================
```

5. **Подтверждение свопа**
```
=== SWAP CONFIRMATION ===
Pool: 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2
Operation: BUY
Amount In: 0.001000000 SOL
Expected Out: 78965.43210 TOKEN
Price: 0.000000013 SOL per token
========================

Do you want to execute this swap? (y/n): 
```
Введите `y` и нажмите Enter

6. **Ввод slippage**
```
Enter maximum slippage tolerance (%) [default: 0.5]: 
```
Нажмите Enter для 0.5% или введите своё значение (например `1`)

7. **Параметры свопа**
```
=== SWAP PARAMETERS ===
Slippage Tolerance: 0.50%
Expected Out: 78965.43210
Minimum Out: 78570.40474
======================
```

8. **Выполнение**
```
Sending transaction...
Waiting for confirmation...
✅ Swap executed successfully!
Transaction: 3xaBc...DeF

Fetching transaction details...
```

9. **Финальный отчёт**
```
=== TRANSACTION REPORT ===
Status: Success
Transaction: 3xaBc...DeF
Explorer: https://solscan.io/tx/3xaBc...DeF

Swap Details:
  Amount In: 0.001000000 SOL
  Amount Out: 78965.43210 TOKEN

Price Analysis:
  Expected Price: 0.000000013 SOL per token
  Actual Price: 0.000000013 SOL per token
  Price Impact: 0.0100%
========================
```

## 6. Проверка результатов

1. **Откройте ссылку на explorer** из отчёта
2. **Проверьте детали транзакции**:
   - Status: Success
   - Program: Raydium V4
   - Token changes
3. **Проверьте балансы** в вашем кошельке

## 7. Тестирование продажи

Если вы купили токены в предыдущем шаге:

```bash
# Продать половину купленных токенов
./main -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 39000 -side sell -execute
```

## 8. Возможные ошибки и решения

### "Failed to load wallet"
```bash
# Проверьте переменную окружения
echo $SOLANA_PRIVATE_KEY
# Должен показать ваш ключ
```

### "insufficient funds"
- Пополните кошелёк SOL
- Учтите комиссии (~0.002 SOL на транзакцию)

### "slippage tolerance exceeded"
- Увеличьте slippage при запросе (введите 1 или 2 вместо 0.5)

### "failed to get pool account"
- Проверьте правильность адреса пула
- Используйте один из проверенных пулов из списка

## 9. Полезные пулы для тестов

```bash
# BONK-SOL (высокая ликвидность, дешёвый токен)
-pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2

# USDC-SOL (стабильная монета)
-pool EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v

# RAY-SOL (токен Raydium)
-pool AVs9TA4nWDzfPJE9gGVNJMVhcQy3V9PGazuz33BfG2RA
```

## 10. Советы по безопасности

1. **Используйте тестовый кошелёк** с минимальными средствами
2. **Начинайте с малых сумм** (0.001 SOL)
3. **Всегда проверяйте детали** перед подтверждением
4. **Сохраняйте хеши транзакций** для отслеживания
5. **Не делитесь приватным ключом**

## Примеры команд для копирования

```bash
# Установка тестового ключа
export SOLANA_PRIVATE_KEY=3bsbhwanLnyjnZ3t4gHKkR8RMdaL3vHTDrsHPqDW4stkG5S4temxqewKr3VfumdfT5p8YRUYMAUfna3xPCeNxmtE

# Компиляция
go build main.go

# Котировка без выполнения
./main -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 0.001 -side buy

# Выполнение свопа
./main -pool 58oQChx4yWmvKdwLLZzBi4ChoCc2fqCUWBkwMihLYQo2 -amount 0.001 -side buy -execute
```