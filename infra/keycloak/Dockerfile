FROM quay.io/keycloak/keycloak:26.7.1

# Build the optimized server with PostgreSQL as the database vendor and health
# endpoints enabled. The db vendor and health-enabled are build-time options, so
# they must be set here rather than at runtime with --optimized. All sensitive
# values are supplied at runtime via environment variables in infra/compose.yaml;
# nothing secret is baked into this image.
RUN /opt/keycloak/bin/kc.sh build --db=postgres --health-enabled=true
