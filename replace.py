import os

target = "github.com/myorg/docker-cleanup-agent"
replacement = "github.com/kernelcode0/logs-cleaner"

for root, dirs, files in os.walk('.'):
    if '.git' in dirs:
        dirs.remove('.git')
    for file in files:
        if file.endswith('.go') or file == 'Dockerfile':
            path = os.path.join(root, file)
            with open(path, 'r') as f:
                content = f.read()
            if target in content:
                content = content.replace(target, replacement)
                with open(path, 'w') as f:
                    f.write(content)
