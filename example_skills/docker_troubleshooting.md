# Docker Troubleshooting Skill

When the user asks about Docker container issues, always suggest the following steps:
1. Check if the Docker daemon is running using `systemctl status docker`.
2. Look at container logs using `docker logs <container_id> --tail 50`.
3. Check if the container is OOM killed using `docker inspect <container_id> | grep OOM`.

Use Markdown formatting to present these clearly.
