FROM node:22-alpine

WORKDIR /app

# Copiamos package.json
COPY package*.json ./

# Instalamos dependencias
RUN npm install

# Copiamos todo el código fuente del frontend
COPY . .

# Exponemos el puerto de Vite (5173)
EXPOSE 5173

# Iniciamos en modo desarrollo, exponiendo al host (--host)
CMD ["npm", "run", "dev", "--", "--host"]