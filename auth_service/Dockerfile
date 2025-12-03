FROM node:22-alpine

WORKDIR /app

# Copiamos package.json primero para aprovechar la caché de Docker
COPY package*.json ./

# Instalamos dependencias
RUN npm install

# Copiamos el resto del código
COPY . .

# Exponemos el puerto del servidor (3000)
EXPOSE 8900

# Comando de inicio
CMD ["node", "server.js"]