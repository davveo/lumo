FROM nginx:1.30.6-alpine

# This context contains only frontend files; backend builds never consume it.
RUN rm -f /etc/nginx/conf.d/default.conf /usr/share/nginx/html/index.html /usr/share/nginx/html/50x.html
COPY nginx/nginx.conf /etc/nginx/nginx.conf
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY nginx/proxy.conf /etc/nginx/lumo-proxy.conf
COPY dist/ /usr/share/nginx/html/

ENV GATEWAY_UPSTREAM=gateway:8080 \
    NGINX_ENVSUBST_FILTER=^GATEWAY_UPSTREAM$

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=6 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/index.html || exit 1
