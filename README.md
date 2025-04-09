## TODO
- Complete readme

## Suggested prompt to make changes
```
The given code belongs to an interceptor proxy in go.

Make the following changes:
[CHANGES]

Code:
[PASTE CODE]
```

To get the code, you can execute the following command (on Linux):
```bash
find . -name "*.go" |while read F; do echo -e "//\n//\n// FILE: $F\n//\n//\n\n" ; cat $F ; echo -e "\n\n"; done |xclip -selection clipboard
```
