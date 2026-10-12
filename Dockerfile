# The binary is built outside this image.
#   CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o campfire ./cmd/campfire
#   docker build -t campfire .
FROM scratch
COPY campfire /campfire
EXPOSE 8080
VOLUME /content
ENTRYPOINT ["/campfire"]
CMD ["-content", "/content", "-addr", ":8080"]
