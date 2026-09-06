#!/bin/bash

# Скрипт для предварительной загрузки популярных городов

echo "📥 Начинаем загрузку популярных городов..."

# Создаем папку для данных
mkdir -p ../data/cities

# Список городов: название, широта, долгота, радиус (м)
cities=(
    "moscow:55.7558:37.6173:30000"
    "spb:59.9343:30.3351:30000"
    "nizhny:56.3287:44.0020:30000"
    "kazan:55.8304:49.0661:30000"
    "ufa:54.7351:55.9587:30000"
    "samara:53.1955:50.1012:30000"
    "ekaterinburg:56.8389:60.6057:30000"
    "novosibirsk:55.0084:82.9357:30000"
)

for city in "${cities[@]}"; do
    IFS=':' read -r name lat lon radius <<< "$city"
    echo "📥 Скачиваем $name..."
    
    # Формируем запрос к Overpass API
    query="[out:json][timeout:60];(way[\"highway\"~\"^(motorway|trunk|primary|secondary|tertiary|residential)\"](around:$radius,$lat,$lon););out geom;"
    
    # URL-кодируем запрос
    encoded_query=$(printf '%s' "$query" | jq -sRr @uri)
    
    # Скачиваем
    curl -s -X GET "https://overpass-api.de/api/interpreter?data=$encoded_query" \
        -H "User-Agent: SushiVeslaBot/1.0" \
        -o "../data/cities/${name}.json"
    
    # Проверяем размер
    size=$(stat -c%s "../data/cities/${name}.json" 2>/dev/null || stat -f%z "../data/cities/${name}.json" 2>/dev/null)
    if [ "$size" -gt 1000 ]; then
        echo "✅ $name скачан (${size} байт)"
    else
        echo "❌ Ошибка скачивания $name"
        rm "../data/cities/${name}.json"
    fi
    
    # Ждем 2 секунды перед следующим запросом
    sleep 2
done

echo "✅ Загрузка завершена!"