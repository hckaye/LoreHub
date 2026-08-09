FROM node:24.18.0-trixie-slim AS dependencies

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:24.18.0-trixie-slim AS build

ENV NEXT_TELEMETRY_DISABLED=1
WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:24.18.0-trixie-slim

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
WORKDIR /app
RUN groupadd --system nextjs && useradd --system --gid nextjs nextjs
COPY --from=build --chown=nextjs:nextjs /app/public ./public
COPY --from=build --chown=nextjs:nextjs /app/.next/standalone ./
COPY --from=build --chown=nextjs:nextjs /app/.next/static ./.next/static
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
