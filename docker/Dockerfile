# Используем официальный легковесный образ Nginx на Alpine Linux
FROM nginx:1.25-alpine

# Устанавливаем метаданные (опционально)
LABEL maintainer="Alexander Grunin<alexander@grunin.ru>"
LABEL description="Nginx с 10-тью картинками в каталоге /images. 001.jpg, 002.jpg...."

# Создаем рабочую директорию для изображений
RUN mkdir -p /usr/share/nginx/html/images
RUN pwd
RUN echo "$PWD"
# Копируем изображения (используем .dockerignore для исключения ненужных файлов)
COPY ./images/ /usr/share/nginx/html/images/

# Копируем оптимизированную конфигурацию Nginx
COPY ./nginx.conf /etc/nginx/nginx.conf

# Проверяем корректность конфигурации (валидация перед запуском)
RUN nginx -t

# Открываем порт HTTP (80) и HTTPS (443) - хотя HTTPS не используется, для совместимости
EXPOSE 80

# Запускаем Nginx в foreground режиме (обязательно для Docker)
CMD ["nginx", "-g", "daemon off;"]