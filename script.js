document.addEventListener('DOMContentLoaded', function() {
    randomBackground();
    const content = document.querySelector('.content');
    const contentDetail = document.querySelector('.about-content');
    const linksContent = document.querySelector('.links-content');

    const aboutLink = document.querySelector('.about-link');
    const linksLink = document.querySelector('.links-link');
    const profilePic = document.querySelector('.circular-image');

    const TRANSFORM_STYLE = 'translateY(-20%) scale(0.8)';

    aboutLink.addEventListener('click', function(event) {
        event.preventDefault();
        if (!content.style.transform) {
            content.style.transform = TRANSFORM_STYLE;
        }
        linksContent.classList.remove('visible');
        linksContent.classList.add('displayNone');

        contentDetail.classList.add('visible');
        contentDetail.classList.remove('displayNone');
    });

    linksLink.addEventListener('click', function(event) {
        event.preventDefault();
        if (!content.style.transform) {
            content.style.transform = TRANSFORM_STYLE;
        }
        contentDetail.classList.remove('visible');
        contentDetail.classList.add('displayNone');

        linksContent.classList.add('visible');
        linksContent.classList.remove('displayNone');
    });

    profilePic.addEventListener('click', function() {
        content.style.transform = '';
        contentDetail.classList.remove('visible');
        contentDetail.classList.add('displayNone');
        linksContent.classList.remove('visible');
        linksContent.classList.add('displayNone');
    });
});

function randomBackground() {
    const numImages = 17;
    const randomBgImage = Math.floor(Math.random() * numImages);
    document.body.style.backgroundImage = `url('backgrounds/${randomBgImage}.jpg')`;
}