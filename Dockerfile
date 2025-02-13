FROM gitea.elara.ws/elara6331/static-root:latest
COPY distrohop /bin/distrohop
ENTRYPOINT [ "/bin/distrohop" ]