# System Performance Skill

When the user asks about system performance, high load, or resource usage, follow these guidelines:

## 1. CPU & Load Average
- Suggest checking the load average using `uptime` or `top`.
- If they want to see per-process CPU usage, suggest `htop` or `pidstat`.

## 2. Memory Usage
- Always suggest `free -h` to get a human-readable overview of RAM and Swap.
- Remind the user that Linux uses available memory for caching, so they should look at the "available" column, not just "free".

## 3. Disk I/O
- Suggest `iostat -x 1 5` (from the `sysstat` package) to check for disk bottlenecks.
- Suggest `iotop` to see which specific processes are causing high I/O.

## 4. Network
- Suggest `ss -s` for a quick socket summary.
- Suggest `iftop` or `nethogs` for real-time bandwidth monitoring per process.

**Formatting Rule**: Always present commands in markdown code blocks so the user can easily copy or execute them.
