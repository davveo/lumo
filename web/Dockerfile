ARG NGINX_IMAGE=nginx:1.30.6-alpine
FROM ${NGINX_IMAGE}

# This context contains only frontend files; backend builds never consume it.
RUN rm -f /etc/nginx/conf.d/*.conf \
    && rm -rf /usr/share/nginx/html \
    && mkdir -p /usr/share/nginx/html
COPY nginx/nginx.conf /etc/nginx/nginx.conf
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY nginx/proxy.conf /etc/nginx/lumo-proxy.conf
COPY entrypoint.sh /lumo-web-entrypoint.sh
COPY dist/ /usr/share/nginx/html/

ENV GATEWAY_UPSTREAM=gateway:8080

ENTRYPOINT ["/bin/sh", "/lumo-web-entrypoint.sh"]
CMD ["nginx", "-e", "/dev/stderr", "-g", "daemon off;"]

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=6 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/index.html || exit 1
