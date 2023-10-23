document.addEventListener('DOMContentLoaded', function() {
    randomBackground();

    const content = document.querySelector('.content');
    const aboutContent = document.querySelector('.about-content');
    const linksContent = document.querySelector('.links-content');

    const aboutLink = document.querySelector('.about-link');
    const linksLink = document.querySelector('.links-link');
    const profilePic = document.querySelector('.circular-image');

    function resetContent() {
        content.classList.remove('content-active');
        aboutContent.classList.remove('content-show');
        linksContent.classList.remove('content-show');
    }

    aboutLink.addEventListener('click', function(event) {
        event.preventDefault();
        content.classList.add('content-active');
        linksContent.classList.remove('content-show');
        aboutContent.classList.add('content-show');
    });

    linksLink.addEventListener('click', function(event) {
        event.preventDefault();
        content.classList.add('content-active');
        aboutContent.classList.remove('content-show');
        linksContent.classList.add('content-show');
    });

    profilePic.addEventListener('click', function() {
        resetContent();
    });
});

function randomBackground() {
    const numImages = 17;
    const randomBgImage = Math.floor(Math.random() * numImages);
    document.body.style.backgroundImage = `url('backgrounds/${randomBgImage}.jpg')`;
}
