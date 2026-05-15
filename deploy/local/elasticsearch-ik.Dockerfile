ARG ELASTICSEARCH_VERSION=8.15.3
FROM docker.elastic.co/elasticsearch/elasticsearch:${ELASTICSEARCH_VERSION}

ARG ELASTICSEARCH_VERSION=8.15.3
ARG IK_PLUGIN_URL=

RUN set -eux; \
    if [ -z "${IK_PLUGIN_URL}" ]; then \
      IK_PLUGIN_URL="https://get.infini.cloud/elasticsearch/analysis-ik/${ELASTICSEARCH_VERSION}"; \
    fi; \
    elasticsearch-plugin install --batch "${IK_PLUGIN_URL}"
