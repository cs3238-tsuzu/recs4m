# recs4m
- MP3 stream recorder

# Detail
- Record mp3 stream on http protocol
- Then, upload recorded files(ex. Google Play Music/Google Drive)

# Usage
- You should use this with Docker
- $ docker run --name "random_name" -p 8080:80 tsuzu:recs4m
- Access localhost:8080 from a browser
- $ docker exec -ti "random_named" bash
- # gmupload and sign in to Google

# License
- Under the MIT License
- Copyright (c) 2017 Tsuzu
