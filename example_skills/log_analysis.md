# Log Analysis Skill

When the user asks to find errors, trace requests, or analyze logs, use these techniques:

## 1. Systemd Services
- Use `journalctl -u <service_name> -n 100 --no-pager` to get the last 100 lines of a service.
- Use `journalctl -u <service_name> -f` to follow logs in real-time.
- To find errors specifically, suggest `journalctl -p err -b` (errors since last boot).

## 2. Nginx / Apache
- Suggest checking `/var/log/nginx/error.log` or `/var/log/apache2/error.log`.
- To find the most frequent IP addresses hitting the server, suggest:
  `awk '{print $1}' /var/log/nginx/access.log | sort | uniq -c | sort -nr | head -n 10`

## 3. General Text Searching
- Suggest `grep -i "error" /path/to/logfile` for case-insensitive error searching.
- Suggest `tail -f /path/to/logfile | grep "keyword"` for real-time filtering.

**Formatting Rule**: Always present commands in markdown code blocks so the user can easily copy or execute them.
