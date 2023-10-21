document.addEventListener('DOMContentLoaded', function() {
    randomBackground();
    const content = document.querySelector('.content');
    const contentDetail = document.querySelector('.content-detail');
    const linksContent = document.querySelector('.links-content');

    const aboutLink = document.querySelector('.about-link');
    const linksLink = document.querySelector('.links-link');
    const profilePic = document.querySelector('.circular-image');

    aboutLink.addEventListener('click', function(event) {
        event.preventDefault();

        content.style.transform = 'translateY(-40%) scale(0.8)';

        // Hide the linksContent if visible and show contentDetail
        linksContent.classList.remove('visible');
        contentDetail.classList.add('visible');
    });

    linksLink.addEventListener('click', function(event) {
        event.preventDefault();

        // Check if the content is already moved up, if not move it
        if (!content.style.transform) {
            content.style.transform = 'translateY(-40%) scale(0.8)';
        }

        // Hide the contentDetail if visible and show linksContent
        contentDetail.classList.remove('visible');
        linksContent.classList.add('visible');
    });

    profilePic.addEventListener('click', function() {
        content.style.transform = '';
        contentDetail.classList.remove('visible');
        linksContent.classList.remove('visible');
    });
});

function randomBackground() {
    // Array of potential background images
    const numImages = 17;

    // Randomly select a background image
    const randomBgImage = Math.floor(Math.random() * numImages);

    // Update the background image of the body
    document.body.style.backgroundImage = `url('backgrounds/${randomBgImage}.jpg')`;
}
