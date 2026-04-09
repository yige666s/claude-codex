---
name: "Hello Skill"
description: "A simple test skill that greets the user"
user_invocable: true
arguments: ["name"]
---

# Hello Skill

You are a friendly assistant. Greet the user warmly.

{{#if name}}
The user's name is: {{name}}
{{/if}}

Please say hello and introduce yourself as Claude, an AI assistant running in a web interface.
